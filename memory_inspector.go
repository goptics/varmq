package varmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/goptics/varmq/contract"
)

// memQueueSeq numbers the in-memory queues of this process, for their Queue IDs.
var memQueueSeq atomic.Uint64

// memQueue is the QueueInspector of an in-memory queue (BindQueue,
// BindPriorityQueue, WithQueue and friends): storage that keeps no counts of
// its own. The queue handle counts submitted; the worker counts the rest
// through the job (its ackRef points at ref). Listed under GET /queues.
type memQueue struct {
	id string
	q  IBaseQueue

	// listed is set once m is under GET /queues (listMem): from then on
	// every worker that runs a job of m with an ID tracks it in running,
	// so Job and CancelJob find it. ponytail: never cleared on unlisting.
	listed  atomic.Bool
	runMu   sync.Mutex
	running map[string]any   // job ID -> its first live run (memJob): no slice per run
	extra   map[string][]any // job ID -> its other live runs, rare (a reused ID)

	submitted, completed, failed atomic.Uint64
	processing                   atomic.Int64

	ref jobAck // what each of its jobs points to: no storage, only totals
}

// withMemInspector gives eq an in-memory QueueInspector, unless eq's storage
// inspects itself. GET /queues lists it while its worker is listed under
// GET /workers (addMemQueue), so short-lived unnamed workers leave nothing. The ID is
// "memory:<InstanceID>:<n>", n unique in the process; InstanceID is read now.
// The ID is not a worker's Instance Record queue_ids: no other process can
// read it.
func withMemInspector(eq *externalBaseQueue) *externalBaseQueue {
	switch eq.q.(type) {
	case QueueInspector, IAcknowledgeable: // counts its own, or acks bypass the shared jobAck
		return eq
	}

	m := &memQueue{
		id: fmt.Sprintf("memory:%s:%d", InstanceID, memQueueSeq.Add(1)),
		q:  eq.q,
	}
	m.ref.totals = m
	eq.totals = m
	eq.w.addMemQueue(m)

	return eq
}

// added counts a job the queue accepts, before it is stored so a fast
// worker never shows completed above submitted; -1 takes back a refused
// one. Nil-safe.
func (m *memQueue) added(n int) {
	if m != nil {
		m.submitted.Add(uint64(n)) // -1 wraps: a subtraction
	}
}

// memTaken moves a job of m in (+1, handed to the worker) or out (-1,
// finished, skipped or dropped) of its processing count. Nil-safe.
func memTaken(m *memQueue, n int64) {
	if m != nil {
		m.processing.Add(n)
	}
}

// memRun sums memTaken over a run of jobs of one memQueue and adds it once
// the run ends, so a batch costs one contended add, not one per job. A
// batch can mix handles (two WithQueue on one storage): each run goes to
// its own.
type memRun struct {
	m *memQueue
	n int64
}

func (r *memRun) add(m *memQueue, d int64) {
	if m != r.m {
		r.flush()
		r.m = m
	}
	r.n += d
}

func (r *memRun) flush() {
	if r.n != 0 {
		memTaken(r.m, r.n)
		r.n = 0
	}
}

func (m *memQueue) QueueID() string { return m.id }

// Pause, Resume and Paused forward to the storage, so GET /queues and
// PATCH /queues/{id}/actions reach it.
func (m *memQueue) Pause() error  { return pauseQueue(m.q) }
func (m *memQueue) Resume() error { return resumeQueue(m.q) }
func (m *memQueue) Paused() bool  { return isQueuePaused(m.q) }

// QueueStats reports the totals and the jobs pending (in the queue: a job
// cancelled or closed there counts until a worker drops it, so pending can
// exceed the pending page) and processing (handed to the worker: in a
// batch, parked or running, also the rest of a batch parked by a queue
// Pause), as redisq counts a popped, unacknowledged job. Processing moves
// once per batch, so a finished job counts until its batch ends (as
// NumProcessing). Finished jobs are not kept in memory, so
// counts.completed and counts.failed are 0; totals.completed counts
// successful jobs only, as redisq's does.
func (m *memQueue) QueueStats(context.Context) (QueueStats, error) {
	s := QueueStats{
		Counts: JobCounts{Pending: int64(m.q.Len()), Processing: max(0, m.processing.Load())},
		Totals: JobTotals{Submitted: m.submitted.Load(), Completed: m.completed.Load(), Failed: m.failed.Load()},
		Paused: m.Paused(), // the storage's word, not inferred from PausedAt
	}
	if p, ok := m.q.(interface{ PausedAt() int64 }); ok && s.Paused {
		s.PausedAt = p.PausedAt() // 0, so left out, if a Resume came in between
	}

	return s, nil
}

// Jobs lists pending jobs in hand-out order (a priority queue's Values
// is sorted so); other states hold none in memory, so their pages are
// empty. Only waiting jobs count: a closed one still in the queue is
// skipped and offset counts past it, and the page fills up to limit from
// one copy of the queue, so a short page means the end of that copy (one
// that starts mid-read is dropped and the next fills its slot).
// ponytail: one copy per page (Values), sorted for a priority queue,
// O(n log n): fine for the admin route; a paged cursor read if in-memory
// queues of millions get inspected often.
func (m *memQueue) Jobs(_ context.Context, state JobState, offset, limit int) ([]JobRecord, error) {
	if state != JobPending || offset < 0 || limit < 1 || offset >= m.q.Len() { // before copying the queue
		return []JobRecord{}, nil
	}

	return pageWaiting(m.q.Values(), offset, limit), nil
}

// pageWaiting is the records of items' waiting entries, skipping the
// first offset of them, up to limit. A varmq job is kept only if
// it waits both before and after its data is read (once): one closed,
// started or ended is not pending. The read can still meet a job that
// starts mid-read (see Job). Other stored items are kept as they are.
func pageWaiting(items []any, offset, limit int) []JobRecord {
	out := []JobRecord{}
	for _, v := range items {
		if len(out) == limit {
			break
		}
		if !waitingItem(v) {
			continue
		}
		if offset > 0 {
			offset--
			continue
		}
		if r := pendingRecord(v); waitingItem(v) {
			out = append(out, r)
		}
	}
	return out
}

// pendingRecord is the Job Record of a stored item: a varmq job, or anything
// custom storage holds (its JSON, or null when it has none).
func pendingRecord(v any) JobRecord {
	r := JobRecord{State: JobPending, Payload: json.RawMessage("null")}
	if j, ok := v.(interface{ recordOf() (string, any) }); ok {
		var data any
		r.ID, data = j.recordOf()
		v = data
	}
	if b, err := json.Marshal(v); err == nil {
		r.Payload = b
	}

	return r
}

// served is set once a REST mux is built (NewServerMux, so Handler):
// in-memory runs are tracked only then. Jobs already running when it is
// set stay untracked until they end.
var served atomic.Bool

// tracked reports whether runs of m's jobs with an ID are tracked: m is
// listed and a REST mux exists. Nil-safe.
func (m *memQueue) tracked() bool {
	return m != nil && m.listed.Load() && served.Load()
}

// listMem lists m under GET /queues and turns tracking of its runs on
// (once a mux is served), so its running jobs can be read and cancelled
// by ID. Only jobs of a listed queue (a named worker's) with an ID pay.
func listMem(m *memQueue) {
	registerQueue(m)
	m.listed.Store(true)
}

// track adds a live run of job j with ID id.
func (m *memQueue) track(id string, j any) {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	if m.running == nil {
		m.running = map[string]any{}
	}
	if _, ok := m.running[id]; !ok {
		m.running[id] = j
		return
	}
	if m.extra == nil {
		m.extra = map[string][]any{}
	}
	m.extra[id] = append(m.extra[id], j)
}

// untrack drops j's run, not another run under the same ID.
func (m *memQueue) untrack(id string, j any) {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	js := m.extra[id]
	if m.running[id] == j {
		if len(js) == 0 {
			delete(m.running, id)
			return
		}
		m.running[id], js = js[0], js[1:]
	} else if i := slices.Index(js, j); i >= 0 {
		js = slices.Delete(js, i, i+1)
	}
	if len(js) == 0 {
		delete(m.extra, id)
		return
	}
	m.extra[id] = js
}

// runsOf is the live runs of ID id.
func (m *memQueue) runsOf(id string) []any {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	j, ok := m.running[id]
	if !ok {
		return nil
	}
	return append([]any{j}, m.extra[id]...)
}

// memJob is a varmq job as an in-memory queue or a worker holds it.
type memJob interface {
	recordOf() (string, any)
	phase() status
	memTotals() *memQueue
	Cancel() error
}

var (
	_ memJob = (*job[int])(nil)
	_ memJob = (*errorJob[int])(nil)
	_ memJob = (*resultJob[int, int])(nil)
)

// find returns m's jobs with ID id: waiting in the queue (added through
// m: another handle on the same storage counts its own), then running in
// any worker (tracked while m is listed: listMem). Ended ones too. A job
// taken into a batch but not reached yet is in neither.
// ponytail: copies the whole queue (Values); an ID index if in-memory
// queues of millions get read by ID often.
func (m *memQueue) find(id string) []memJob {
	if id == "" {
		return nil
	}

	var out []memJob
	add := func(v any) {
		j, ok := v.(memJob)
		if !ok {
			return
		}
		if jid, _ := j.recordOf(); jid == id && j.memTotals() == m && !slices.Contains(out, j) {
			out = append(out, j)
		}
	}
	for _, v := range m.q.Values() {
		add(v)
	}
	for _, v := range m.runsOf(id) { // moved from the queue meanwhile: listed again, once
		add(v)
	}

	return out
}

// Job is the JobReader of an in-memory queue: a waiting job (pending) or
// a running one (processing). Ended jobs are not kept: ErrJobNotFound.
//
// A running job's payload is null: its worker may write its data while it
// runs (a map: a fatal concurrent read). A waiting job's data is read
// without holding it (the job state machine never waits on a REST read):
// a job that starts during the read may be read while its worker writes
// Data(), as on GET /queues/{id}/jobs. Don't mutate Data() in place if you
// use the job routes. Checked again after the read: one that started is
// reported running, its payload dropped; one closed or ended is not found.
func (m *memQueue) Job(_ context.Context, id string) (JobRecord, error) {
	for _, j := range m.find(id) {
		s := j.phase()
		if waiting(s) {
			r := pendingRecord(j)
			if s = j.phase(); waiting(s) {
				return r, nil
			}
		}
		if s == processing {
			return JobRecord{ID: id, State: JobProcessing, Payload: json.RawMessage("null"), Attempts: 1}, nil
		}
	}

	return JobRecord{}, ErrJobNotFound
}

// waiting reports a job state that has not started: created or queued.
func waiting(s status) bool { return s == created || s == queued }

// waitingItem reports whether stored item v waits: a varmq job created or
// queued, or anything custom storage holds.
func waitingItem(v any) bool {
	j, ok := v.(interface{ phase() status })
	return !ok || waiting(j.phase())
}

// CancelJob is the JobCanceller of an in-memory queue: Cancel on each job
// with ID id, waiting or running. ErrJobNotFound when there is none,
// ErrJobEnded when each one ended.
func (m *memQueue) CancelJob(id string) error {
	js := m.find(id)
	if len(js) == 0 {
		return ErrJobNotFound
	}

	err := contract.ErrJobEnded
	for _, j := range js {
		switch e := j.Cancel(); {
		case e == nil:
			err = nil
		case err != nil && !errors.Is(e, ErrJobAlreadyClosed):
			err = e
		}
	}

	return err
}
