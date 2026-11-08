package varmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goptics/varmq/utils"
)

const (
	// created indicates the job has been created but not yet queued
	created status = iota
	// queued indicates the job is waiting in the queue to be processed
	queued
	// processing indicates the job is currently being executed
	processing
	// finished indicates the job has completed execution
	finished
	// closed indicates the job has been closed and resources freed
	closed
)

var (
	ErrJobProcessing    = errors.New("job is processing, you can't close processing job")
	ErrJobAlreadyClosed = errors.New("job is already closed")
	ErrAcknowledgeJob   = errors.New("queue failed to acknowledge job")
)

// Result represents the result of a job, containing the data and any error that occurred.
type Result[T any] struct {
	JobId string
	Data  T
	Err   error
}

type Identifiable interface {
	// ID returns the unique identifier of the job.
	// Returns empty in case don't set any id by client
	ID() string
}

type StatusProvider interface {
	// IsClosed returns true if the job has been closed.
	IsClosed() bool
	// Status returns a string representation of the job's current lifecycle state.
	// Possible return values:
	// - "Created": Job has been created but not yet added to a queue
	// - "Queued": Job has been added to a queue and is waiting to be processed
	// - "Processing": Job is currently being processed by a worker
	// - "Finished": Job processing has completed successfully
	// - "Closed": Job has been manually closed and will not be processed
	Status() string
}

type Awaitable interface {
	// Wait blocks the caller until the job has completed processing.
	// This method can be used to synchronize execution flow with job completion.
	// If the job has already finished, this method returns immediately.
	Wait()
	// WaitContext is Wait that gives up when ctx ends: ctx.Err() then.
	WaitContext(ctx context.Context) error
}

type Drainer interface {
	// Drain discards the results or errors a group job's channel still holds,
	// so a caller that does not read them does not keep the channel full.
	// A single job keeps its result in place and has no channel: Drain is a
	// no-op there, and Err or Result still return the value.
	Drain()
}

// job represents a task to be executed by a worker. It maintains the task's
// current status, Payload data, and channels for receiving results.
type job[T any] struct {
	id     string
	data   T
	status atomic.Uint32
	wg     sync.WaitGroup
	// ackRef is set by ackSource.Next and taken by acknowledge. An in-memory
	// queue's job shares one ack-less jobAck per queue that only names its
	// QueueInspector (memQueue.ref): no allocation per job.
	ackRef atomic.Pointer[jobAck]
}

// jobAck holds the storage that handed out a job and the ack id it gave.
// One pointer to both keeps in-memory job[T] in the 64-byte size class.
type jobAck struct {
	q  IAcknowledgeable
	id string
	// ext is rare per-job state (failure reason, workflow tag). One pointer
	// keeps jobAck in the 48-byte size class; nil for plain jobs.
	ext *ackExt
	// totals is set, and q nil, on an in-memory queue's shared jobAck.
	totals *memQueue
}

// ackExt is what only some jobs carry.
type ackExt struct {
	// reason is set when the run of a plain job failed: Close then calls
	// Reject with *reason instead of Acknowledge.
	reason *string
	// task is set on a job that is a workflow task.
	task *taskTag
	// end is how the task's run ended; nil until it ran, and a task closed
	// before it ran ends cancelled.
	end *TaskEnd
	// result is a plain result job's R as JSON, for storage that keeps it
	// (ResultAcknowledgeable): Close acknowledges with it.
	result []byte
	// cancelled: the run failed because Cancel stopped it; storage that can
	// (CancelAcknowledgeable) finishes it cancelled, not failed.
	cancelled bool
}

// remoteResultLimit caps the R a result worker leaves on shared storage.
// Bigger: the job still completes, with no result (ErrNoResult).
// ponytail: fixed, a WithResultLimit option if someone needs another.
const remoteResultLimit = 64 << 10

// taskTag names the workflow task a job runs. store is nil until bound.
type taskTag struct {
	wf, task string
	undo     bool // the task's undo job, not the task
	store    WorkflowStore
}

// matches: t is no tag (no wf, task or undo), or one a Submit could write
// for job id: valid ID and name, id "wf:<wf>:<task>" (+":undo"). Anything
// else is unparsable, so no store key is built from it.
func (t taskTag) matches(id string) bool {
	if t.wf == "" {
		return t.task == "" && !t.undo
	}

	want := taskJobID(t.wf, t.task)
	if t.undo {
		want += ":undo"
	}

	return validWorkflowID(t.wf) && validTaskName(t.task) && id == want
}

// jobView is the job envelope persistent and distributed storage holds
// (spec/redis-storage.md, section 3).
type jobView[T any] struct {
	Id         string `json:"id"`
	Status     string `json:"status"`
	Payload    T      `json:"data"`
	EnqueuedAt int64  `json:"enqueued_at,omitempty"` // Unix ms, producer clock
	Wf         string `json:"wf,omitempty"`          // workflow id, workflow task only
	Task       string `json:"task,omitempty"`        // task name within Wf
	Undo       bool   `json:"undo,omitempty"`        // Task's undo job
}

type Job[T any] interface {
	Identifiable
	// Data returns the data associated with the job.
	Data() T
	// Context is cancelled when the job is cancelled (Cancel, from this or
	// any process) while it runs. Watch it to stop a cancelled job.
	Context() context.Context
}

type iJob[T any] interface {
	Job[T]
	StatusProvider
	changeStatus(s status)
	startProcessing() bool
	setAck(q IAcknowledgeable, id string)
	setTask(tag taskTag)
	taskTag() *taskTag
	endTask(o Outcome, reason string, result []byte) *jobAck
	fail(reason string)
	succeed()
	memTotals() *memQueue
	Cancel() error
	ackStorage() any
	endRun()
	endFailed(err error) error
	io.Closer
}

// EnqueuedJob is an interface representing a job that has been successfully enqueued.
// It extends Identifiable, StatusProvider, and Awaitable interfaces to provide
// identification, status tracking, and waiting capabilities. This interface is
// returned by the Add method when used with NewWorker
type EnqueuedJob interface {
	Identifiable
	StatusProvider
	Awaitable
	// Close marks the job as closed without removing it from the queue.
	// When a closed job is dequeued, the worker will skip processing it.
	// This operation takes constant time as it only updates the job's state.
	Close() error
	// Cancel stops the job: one not started never runs; a running one has
	// its Context cancelled. ErrJobAlreadyClosed once it ended.
	Cancel() error
}

// New creates a new job with the provided data.
func newJob[T any](data T, configs jobConfigs) *job[T] {
	j := &job[T]{
		id:     configs.Id,
		data:   data,
		status: atomic.Uint32{},
		wg:     sync.WaitGroup{},
	}

	j.wg.Add(1)

	return j
}

// setAck records the storage and ack id Close acknowledges with. An empty id
// means there is nothing to acknowledge, so nothing is stored. A workflow
// tag parseToJob set is kept, bound to q's workflow store; nothing else of
// the old ack is (a stale reason would reject the job).
// ponytail: one 32-byte allocation per acked job; that path already pays
// json.Unmarshal, so this is not pooled.
func (j *job[T]) setAck(q IAcknowledgeable, id string) {
	if id == "" {
		return
	}

	ref := &jobAck{q: q, id: id}
	if old := j.ackRef.Load(); old != nil && old.ext != nil && old.ext.task != nil {
		tag := *old.ext.task
		if wa, ok := q.(WorkflowAcknowledgeable); ok {
			tag.store = wa.WorkflowStore()
		}
		ref.ext = &ackExt{task: &tag}
	}

	j.ackRef.Store(ref)
}

// taskTag is the workflow task this job runs, nil for a plain job.
func (j *job[T]) taskTag() *taskTag {
	if ref := j.ackRef.Load(); ref != nil && ref.ext != nil {
		return ref.ext.task
	}
	return nil
}

// ackCopy copies old and its ext (zero for nil), for a change stored as a
// new ack: an in-memory queue's jobAck is shared.
func ackCopy(old *jobAck) (ref jobAck, ext ackExt) {
	if old != nil {
		ref = *old
	}
	if ref.ext != nil {
		ext = *ref.ext
	}
	return ref, ext
}

// setTask tags the job as the workflow task (or undo) tag names.
func (j *job[T]) setTask(tag taskTag) {
	ref, ext := ackCopy(j.ackRef.Load())
	ext.task, ref.ext = &tag, &ext
	j.ackRef.Store(&ref)
}

// endTask records how a workflow task's run ended, for Close to report.
// Nothing for a plain job. Returns the ack now held, so callers need not
// load it again.
func (j *job[T]) endTask(o Outcome, reason string, result []byte) *jobAck {
	old := j.ackRef.Load()
	if old == nil || old.ext == nil || old.ext.task == nil {
		return old
	}

	ref, ext := ackCopy(old)
	ext.end, ref.ext = &TaskEnd{Outcome: o, Reason: reason, Result: result}, &ext
	j.ackRef.Store(&ref)

	return &ref
}

// fail records that the run failed, so Close rejects the job with reason
// instead of acknowledging it (a workflow task: finishes it failed), and
// counts it failed for its in-memory queue. The worker calls it before
// Close; a user Close fails while the job is processing, so nothing else
// reads the ack meanwhile.
func (j *job[T]) fail(reason string) {
	ref := j.ackRef.Load()
	switch {
	case ref == nil:
	case ref.ext != nil && ref.ext.task != nil:
		j.endTask(OutcomeFailed, reason, nil)
	case ref.q != nil:
		j.ackRef.Store(&jobAck{q: ref.q, id: ref.id, ext: &ackExt{reason: &reason}})
	}

	if ref != nil && ref.totals != nil {
		ref.totals.failed.Add(1)
	}
}

// succeed counts the job completed for its in-memory queue, if any, and
// finishes a workflow task completed, with no result.
func (j *job[T]) succeed() { j.succeedWith(nil) }

// succeedWith is succeed with the task's Result. A result its in-memory
// store refuses (too large) counts failed, as the store ends the task.
func (j *job[T]) succeedWith(result []byte) {
	ref := j.endTask(OutcomeCompleted, "", result)
	switch {
	case ref == nil || ref.totals == nil:
	case ref.ext != nil && ref.ext.task != nil && ref.ext.task.tooLarge(result):
		ref.totals.failed.Add(1)
	default:
		ref.totals.completed.Add(1)
	}
}

// memTotals is the in-memory queue that counts this job, nil for storage
// that counts its own.
func (j *job[T]) memTotals() *memQueue {
	if ref := j.ackRef.Load(); ref != nil {
		return ref.totals
	}
	return nil
}

// countedBy marks the job as one of m's. Nil m: nothing to count.
func (j *job[T]) countedBy(m *memQueue) {
	if m != nil {
		j.ackRef.Store(&m.ref)
	}
}

// recordOf is what an in-memory queue's Jobs lists for a pending job.
func (j *job[T]) recordOf() (string, any) { return j.id, j.data }

func (j *job[T]) ID() string {
	return j.id
}

func (j *job[T]) Data() T {
	return j.data
}

// State returns the current status of the job as a string.
func (j *job[T]) Status() string {
	switch j.status.Load() {
	case created:
		return "Created"
	case queued:
		return "Queued"
	case processing:
		return "Processing"
	case finished:
		return "Finished"
	case closed:
		return "Closed"
	default:
		return "Unknown"
	}
}

// IsClosed returns true if the job has been closed.
func (j *job[T]) IsClosed() bool {
	return j.status.Load() == closed
}

// changeStatus updates the job's status to the provided value.
func (j *job[T]) changeStatus(s status) {
	j.status.Store(s)
}

func (j *job[T]) Wait() {
	j.wg.Wait()
}

func (j *job[T]) WaitContext(ctx context.Context) error { return waitContext(ctx, j.wg.Wait) }

// waitContext runs wait until it returns or ctx ends.
// ponytail: one goroutine per call, gone when wait returns (every job and
// group ends: run, purged or closed); a done channel if calls pile up.
func waitContext(ctx context.Context, wait func()) error {
	if ctx.Done() == nil {
		wait()
		return nil
	}

	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (j *job[T]) Json() ([]byte, error) {
	view := jobView[T]{
		Id:         j.ID(),
		Status:     j.Status(),
		Payload:    j.data,
		EnqueuedAt: time.Now().UnixMilli(),
	}
	if t := j.taskTag(); t != nil {
		view.Wf, view.Task, view.Undo = t.wf, t.task, t.undo
	}

	return json.Marshal(view)
}

// parseToJob decodes a stored envelope into the job kind mk builds, so an
// error or result worker gets its own kind. Workflow tags are kept.
func parseToJob[T any, J iJob[T]](data []byte, mk func(T, jobConfigs) J) (J, error) {
	var zero J

	var view jobView[T]
	if err := json.Unmarshal(data, &view); err != nil {
		return zero, fmt.Errorf("%w: %w", ErrParseJob, err)
	}

	var st status
	switch view.Status {
	case "Created":
		st = created
	case "Queued":
		st = queued
	case "Processing":
		st = processing
	case "Finished":
		st = finished
	case "Closed":
		st = closed
	default:
		return zero, fmt.Errorf("invalid status: %s", view.Status)
	}

	tag := taskTag{wf: view.Wf, task: view.Task, undo: view.Undo}
	if !tag.matches(view.Id) {
		return zero, fmt.Errorf("%w: bad workflow tag", ErrParseJob)
	}

	j := mk(view.Payload, jobConfigs{Id: view.Id})
	if view.Wf != "" {
		j.setTask(tag) // store bound later
	}
	j.changeStatus(st)

	return j, nil
}

// closeable reports why a job in state s cannot be closed, or nil.
func closeable(s status) error {
	switch s {
	case processing:
		return ErrJobProcessing
	case closed:
		return ErrJobAlreadyClosed
	}

	return nil
}

// notClosed reports ErrJobAlreadyClosed for a closed job, nil otherwise.
func notClosed(s status) error {
	if s == closed {
		return ErrJobAlreadyClosed
	}

	return nil
}

// transition CASes the job to state to if allowed accepts its current state,
// retrying while other goroutines move it. Of two concurrent callers exactly
// one wins; the other sees the new state.
func (j *job[T]) transition(to status, allowed func(status) error) error {
	for {
		s := j.status.Load()
		if err := allowed(s); err != nil {
			return err
		}

		if j.status.CompareAndSwap(s, to) {
			return nil
		}
	}
}

// claimClose moves a created, queued or finished job to closed, so only one
// concurrent Close can win and run the close side effects.
func (j *job[T]) claimClose() error {
	return j.transition(closed, closeable)
}

// markQueued records that Enqueue succeeded. It only moves a created job: a
// worker may already have started it, or the user closed it, before this runs.
func (j *job[T]) markQueued() {
	j.status.CompareAndSwap(created, queued)
}

// startProcessing moves a job a worker took to processing, unless it was
// closed meanwhile.
func (j *job[T]) startProcessing() bool {
	return j.transition(processing, notClosed) == nil
}

// closeOnce acknowledges the job's storage, if any, then claims the move to
// closed. acknowledge runs at most once and the claim is one CAS, so of two
// concurrent Close calls exactly one acknowledges and exactly one wins; the
// other gets ErrJobAlreadyClosed. Acking before the claim means the job never
// shows Closed while unacked, so a failed ack leaves it untouched: a worker
// that took it still runs it, and a finished one stays Finished and unwaited.
//
// An in-memory workflow task reports to its store after the claim: the store
// never refuses, the claim already makes it once, and the ack stays for
// memTotals.
func (j *job[T]) closeOnce() error {
	ref := j.ackRef.Load()
	if ref != nil && ref.q != nil {
		if err := closeable(j.status.Load()); err != nil {
			return err
		}

		if err := j.acknowledge(); err != nil {
			return err
		}
	}

	if err := j.claimClose(); err != nil {
		return err
	}

	// Loaded again: a Close that loaded before the run's end was recorded
	// may win the claim, and must report that end, not cancelled.
	if ref := j.ackRef.Load(); ref != nil && ref.q == nil && ref.ext != nil && ref.ext.task != nil {
		finishMemTask(ref.ext)
	}

	return nil
}

// Close closes the job, whatever its state except processing, and releases
// Wait. Only the call that wins the transition releases; a concurrent Close
// returns ErrJobAlreadyClosed. See closeOnce for acknowledgeable storage.
func (j *job[T]) Close() error {
	if err := j.closeOnce(); err != nil {
		return err
	}

	j.wg.Done()

	return nil
}

// acknowledge reports the job's outcome to its storage at most once:
// Reject after fail, Acknowledge otherwise. It takes the ack out of the job
// first, so a concurrent or later call finds nothing to do, and puts it back
// only when storage refuses, so a retry can report again.
func (j *job[T]) acknowledge() error {
	ref := j.ackRef.Swap(nil)
	if ref == nil {
		return nil
	}

	var wa WorkflowAcknowledgeable
	if ref.ext != nil && ref.ext.task != nil {
		wa, _ = ref.q.(WorkflowAcknowledgeable)
	}

	var ok bool
	if wa != nil {
		// Poke even when !ok. A re-finish of an ended task returns every
		// push-target queue of the workflow: the reply that pushed may be
		// lost.
		var pushed []string
		pushed, ok = wa.FinishTask(ref.id, taskEnd(ref.ext))
		pokeLocal(pushed)
	} else if reason, failed := failReason(ref.ext); failed {
		if ca, can := ref.q.(CancelAcknowledgeable); can && ref.ext.cancelled {
			ok = ca.AcknowledgeCancelled(ref.id, reason)
		} else {
			ok = ref.q.Reject(ref.id, reason)
		}
	} else if ra, has := ref.q.(ResultAcknowledgeable); has && ref.ext != nil && ref.ext.result != nil {
		// A result storage refuses (e.g. JSON its decoder can't take) must
		// not keep the job unacked, redelivered forever: complete it
		// without (ErrNoResult).
		if ok = ra.AcknowledgeResult(ref.id, ref.ext.result); !ok {
			ok = ref.q.Acknowledge(ref.id)
		}
	} else {
		ok = ref.q.Acknowledge(ref.id)
	}

	if !ok {
		j.ackRef.Store(ref)
		return fmt.Errorf("%w: jobId=%s, ackId=%s", ErrAcknowledgeJob, j.id, ref.id)
	}

	return nil
}

// failReason is why a run failed, for Reject: a plain job's reason, or a
// task's failed end on storage that can't finish tasks (old storage).
func failReason(ext *ackExt) (string, bool) {
	switch {
	case ext == nil:
		return "", false
	case ext.reason != nil:
		return *ext.reason, true
	case ext.end != nil && ext.end.Outcome == OutcomeFailed:
		return ext.end.Reason, true
	}

	return "", false
}

type iErrorJob[T any] interface {
	iJob[T]
	sendError(err error)
}

// errorJob is a single job whose worker reports an error. The error is stored
// in place and published by the wg.Done in job.Close, so the job itself is the
// one-shot latch: no channel per job.
type errorJob[T any] struct {
	job[T]
	err error
}

// EnqueuedErrJob is an interface representing an error job that has been successfully enqueued.
// It extends EnqueuedJob and Drainer interfaces to provide identification, status tracking,
// waiting, and cleanup capabilities. This interface is returned by the Add method when used
// with NewErrWorker, allowing clients to retrieve any error through the Err() method.
type EnqueuedErrJob interface {
	EnqueuedJob
	Drainer
	// Err retrieves any error that occurred during job execution.
	// This method blocks the caller until the job processing is complete.
	// If the job has already finished, it returns immediately with the error result.
	// A nil return value indicates successful job execution without errors.
	Err() error
}

func newErrorJob[T any](data T, configs jobConfigs) *errorJob[T] {
	e := &errorJob[T]{
		job: job[T]{
			id:     configs.Id,
			data:   data,
			status: atomic.Uint32{},
			wg:     sync.WaitGroup{},
		},
	}
	e.wg.Add(1)
	return e
}

// Err blocks until the job is Closed (processed, purged or closed by the user)
// and returns the error sent before that, or nil.
func (ej *errorJob[T]) Err() error {
	ej.wg.Wait()
	return ej.err
}

// sendError stores the error. The worker calls it before Close, so the write
// happens-before the wg.Done that releases Err.
func (ej *errorJob[T]) sendError(err error) {
	ej.err = err
}

// Drain is a no-op: a single job has no channel to drain. It exists so the job
// satisfies the Drainer embedded in EnqueuedErrJob.
func (ej *errorJob[T]) Drain() {}

type iResultJob[T, R any] interface {
	iErrorJob[T]
	sendResult(result R)
	succeedResult() error
}

// resultJob is a single job whose worker reports a result or an error, stored
// in place like errorJob.err. It holds data and err, not a Result[R], whose
// JobId would be an unused copy of job.id and push it out of the 80-byte
// size class.
type resultJob[T, R any] struct {
	job[T]
	data R
	err  error
}

// EnqueuedResultJob is an interface representing a result job that has been successfully enqueued.
// It extends EnqueuedJob and Drainer interfaces to provide identification, status tracking,
// waiting, and cleanup capabilities. This interface is returned by the Add method when used
// with NewResultWorker, allowing clients to retrieve the job result through the Result() method.
type EnqueuedResultJob[R any] interface {
	EnqueuedJob
	Drainer
	// Result retrieves the computed result value and any error from the job execution.
	// This method blocks the caller until the job processing is complete.
	// If the job has already finished, it returns immediately with the result.
	Result() (R, error)
}

func newResultJob[T, R any](data T, configs jobConfigs) *resultJob[T, R] {
	r := &resultJob[T, R]{
		job: job[T]{
			id:     configs.Id,
			data:   data,
			status: atomic.Uint32{},
			wg:     sync.WaitGroup{},
		},
	}
	r.wg.Add(1)
	return r
}

// Result blocks until the job is Closed (processed, purged or closed by the
// user) and returns what was sent before that, or the zero value and nil.
func (rj *resultJob[T, R]) Result() (R, error) {
	rj.wg.Wait()
	return rj.data, rj.err
}

// sendResult stores the result. The worker calls it before Close, so the write
// happens-before the wg.Done that releases Result.
func (rj *resultJob[T, R]) sendResult(result R) {
	rj.data = result
}

// sendError stores the error, see sendResult.
func (rj *resultJob[T, R]) sendError(err error) {
	rj.err = err
}

// Drain is a no-op: a single job has no channel to drain. It exists so the job
// satisfies the Drainer embedded in EnqueuedResultJob.
func (rj *resultJob[T, R]) Drain() {}

// keepResult stores R as JSON for Close to acknowledge with, when the job
// came from storage that keeps results. R that does not marshal, or is over
// remoteResultLimit, is left out: the job completes with no result. A
// MarshalJSON that panics is returned: the run fails with it.
func (rj *resultJob[T, R]) keepResult() error {
	ref := rj.ackRef.Load()
	if ref == nil {
		return nil
	}
	if _, ok := ref.q.(ResultAcknowledgeable); !ok {
		return nil
	}

	var b []byte
	var err error
	if p := utils.WithSafe("result MarshalJSON", func() { b, err = json.Marshal(rj.data) }); p != nil {
		return p
	}
	if err != nil || len(b) > remoteResultLimit {
		return nil
	}

	nref, ext := ackCopy(ref)
	ext.result, nref.ext = b, &ext
	rj.ackRef.Store(&nref)
	return nil
}

// succeedResult is succeed for a result job. A workflow task (not an undo)
// leaves R as JSON, its Result; shared storage that keeps results gets R
// via keepResult. It returns the error the run fails with instead: R of a
// task that does not marshal, or a MarshalJSON that panics (after the
// worker's recover, so caught here).
func (rj *resultJob[T, R]) succeedResult() error {
	t := rj.taskTag()
	if t == nil {
		if err := rj.keepResult(); err != nil {
			return err
		}
		rj.succeedWith(nil)
		return nil
	}
	if t.undo {
		rj.succeedWith(nil)
		return nil
	}

	var b []byte
	var err error
	if p := utils.WithSafe("result MarshalJSON", func() { b, err = json.Marshal(rj.data) }); p != nil {
		err = p
	}
	if err != nil {
		return fmt.Errorf("varmq: task result: %w", err)
	}

	rj.succeedWith(b)
	return nil
}
