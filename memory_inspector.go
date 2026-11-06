package varmq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
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

func (m *memQueue) QueueID() string { return m.id }

// QueueStats reports the totals and the jobs pending (in the queue) and
// processing (handed to the worker: in a batch, parked or running), as
// redisq counts a popped, unacknowledged job. Finished jobs are not kept in
// memory, so counts.completed and counts.failed are 0; totals.completed
// counts successful jobs only, as redisq's does.
func (m *memQueue) QueueStats(context.Context) (QueueStats, error) {
	return QueueStats{
		Counts: JobCounts{Pending: int64(m.q.Len()), Processing: max(0, m.processing.Load())},
		Totals: JobTotals{Submitted: m.submitted.Load(), Completed: m.completed.Load(), Failed: m.failed.Load()},
	}, nil
}

// Jobs lists pending jobs in hand-out order; other states hold none in
// memory, so their pages are empty.
// ponytail: Values copies the whole queue per page; a paged storage read if
// in-memory queues of millions get inspected often.
func (m *memQueue) Jobs(_ context.Context, state JobState, offset, limit int) ([]JobRecord, error) {
	out := []JobRecord{}
	if state != JobPending {
		return out, nil
	}

	if offset >= m.q.Len() { // before copying the queue
		return out, nil
	}
	values := m.q.Values()
	if offset >= len(values) {
		return out, nil
	}
	// no offset+limit: offset may be near MaxInt
	for _, v := range values[offset : offset+min(limit, len(values)-offset)] {
		out = append(out, pendingRecord(v))
	}

	return out, nil
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
