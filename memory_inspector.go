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

// withMemInspector gives eq an in-memory QueueInspector and lists it under
// GET /queues, unless eq's storage inspects itself. The ID is
// "memory:<InstanceID>:<n>", n unique in the process; InstanceID is read now.
// The ID is not a worker's Instance Record queue_ids: no other process can
// read it.
func withMemInspector(eq *externalBaseQueue) *externalBaseQueue {
	if _, ok := eq.q.(QueueInspector); ok {
		return eq
	}

	m := &memQueue{
		id: fmt.Sprintf("memory:%s:%d", InstanceID, memQueueSeq.Add(1)),
		q:  eq.q,
	}
	m.ref.totals = m
	eq.totals = m
	registerQueue(eq.totals)

	return eq
}

// added counts a job the queue accepted. Nil-safe.
func (m *memQueue) added() {
	if m != nil {
		m.submitted.Add(1)
	}
}

func (m *memQueue) QueueID() string { return m.id }

// QueueStats reports pending and processing jobs and the totals. Finished
// jobs are not kept in memory, so counts.completed and counts.failed are 0.
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

	values := m.q.Values()
	for _, v := range values[min(offset, len(values)):min(offset+limit, len(values))] {
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
