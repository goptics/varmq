package varmq

import "encoding/json"

// IBaseQueue is the root interface of queue operations. workers queue needs to implement this interface.
type IBaseQueue interface {
	Len() int
	Dequeue() (any, bool)
	Values() []any
	Purge()
	Close() error
}

// IQueue is the root interface of queue operations.
type IQueue interface {
	IBaseQueue
	Enqueue(item any) bool
}

// IPriorityQueue is the root interface of priority queue operations.
type IPriorityQueue interface {
	IBaseQueue
	Enqueue(item any, priority int) bool
}

// IDedupQueue is optional for IQueue storage. A queue adds a job that has an
// id with EnqueueOnce, which stores item unless an item with the same id is
// already stored.
//
// Persistent and distributed storage keeps an id for a window it sets and
// returns true for a repeat too. A storage client's re-send of a push whose
// reply was lost, or an Add retried with the same id, then stores the job
// once. A repeat still counts as submitted.
//
// In-memory storage (BindQueue, WithQueue) keeps an id while its item is in
// the queue, until Dequeue or Purge takes it, and returns false for a repeat:
// Add hands back the job, which would never finish if it was not stored. The
// in-memory item is the job itself, and its ID method returns id. An
// in-memory Add calls EnqueueOnce only for an id the caller gave (WithJobId,
// Item.ID), not a generated one.
type IDedupQueue interface {
	EnqueueOnce(item any, id string) bool
}

// IDedupPriorityQueue is IDedupQueue for IPriorityQueue storage.
type IDedupPriorityQueue interface {
	EnqueueOnce(item any, priority int, id string) bool
}

// enqueue stores item in q, once per id when the job has one and q
// implements IDedupQueue. The id check comes first, so a job without one
// pays no type assertion.
func enqueue(q IQueue, item any, id string) bool {
	if id != "" {
		if d, ok := q.(IDedupQueue); ok {
			return d.EnqueueOnce(item, id)
		}
	}
	return q.Enqueue(item)
}

// enqueuePriority is enqueue for priority storage (see IDedupPriorityQueue).
func enqueuePriority(q IPriorityQueue, item any, priority int, id string) bool {
	if id != "" {
		if d, ok := q.(IDedupPriorityQueue); ok {
			return d.EnqueueOnce(item, priority, id)
		}
	}
	return q.Enqueue(item, priority)
}

// IAcknowledgeable is the root interface of acknowledgeable operations.
//
// A worker reports each job's outcome once, when the job closes: Acknowledge
// on success, Reject on its final failure. A failure with retries left calls
// neither. varmq has no retries today, so every failure is final and calls
// Reject. See spec/redis-storage.md for what storage does with each.
type IAcknowledgeable interface {
	// Acknowledge marks the job completed. Returns true if the item was
	// successfully acknowledged, false otherwise.
	Acknowledge(ackID string) bool
	// Reject marks the job failed for good: storage removes it without
	// requeueing it. reason is the error text. Returns false if storage
	// could not record it; the job then stays unclosed, as for Acknowledge.
	Reject(ackID string, reason string) bool
	// DequeueWithAckId dequeues an item from the queue
	// Returns the item, a boolean indicating if the operation was successful, and the acknowledgment ID.
	DequeueWithAckId() (any, bool, string)
}

// IBatchAcknowledgeable is optional for acknowledgeable storage. A worker
// whose only source implements it reads that source through one pop per
// dispatch, with no length probes in between.
type IBatchAcknowledgeable interface {
	IAcknowledgeable
	// DequeueBatchWithAckIds moves up to min(len(items), len(ackIDs)) items
	// to pending in one atomic step, each under its own ack id, into
	// items[:n] and ackIDs[:n]. remaining is the storage length right after
	// the move, or a lower bound when the storage caps its count; it is above
	// 0 whenever items remain. n is 0 with a nil error when the storage is
	// empty. A short pop (n below the count) normally means the storage ran
	// dry; storage may also return a short pop with remaining > 0, for
	// example items it hands back from an earlier failed call. err is a
	// failure: the worker reports it and pulls again on the next sign of
	// work.
	//
	// A worker bound to it as a distributed queue does not probe the storage
	// on its own: after a short or failed pop it waits for an "enqueued"
	// notification. A subscribable implementation must therefore also send
	// "enqueued" for items no Enqueue announced (visibility-timeout requeues,
	// other clients' pushes, items left after a failed call), for example
	// once a second while items wait, as redisq does.
	DequeueBatchWithAckIds(items []any, ackIDs []string) (n, remaining int, err error)
}

type IPersistentQueue interface {
	IQueue
	IAcknowledgeable
}

// IPersistentPriorityQueue is the root interface of persistent priority queue operations.
type IPersistentPriorityQueue interface {
	IPriorityQueue
	IAcknowledgeable
}

// ISubscribable is the root interface of subscribable operations.
type ISubscribable interface {
	Subscribe(func(action string))
}

// IDistributedQueue is the root interface of distributed queue operations.
type IDistributedQueue interface {
	IPersistentQueue
	ISubscribable
}

// IDistributedPriorityQueue is the root interface of distributed priority queue operations.
type IDistributedPriorityQueue interface {
	IPersistentPriorityQueue
	ISubscribable
}

// CapacitySetter is the interface for setting the capacity of a queue. (Optional)
type CapacitySetter interface {
	SetCapacity(cap int)
}

// JobState is where a job is in its life (spec/openapi.yaml, JobState).
type JobState string

const (
	JobPending    JobState = "pending"    // in the queue, not handed out
	JobProcessing JobState = "processing" // handed out, no outcome yet
	JobCompleted  JobState = "completed"  // acknowledged
	JobFailed     JobState = "failed"     // rejected
)

// JobCounts is how many jobs a queue holds in each Job State right now.
// Completed and Failed count the Job Records storage still keeps, which
// retention bounds; JobTotals counts every job.
type JobCounts struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Completed  int64 `json:"completed"`
	Failed     int64 `json:"failed"`
}

// JobTotals counts jobs since the queue's storage began counting.
type JobTotals struct {
	Submitted uint64 `json:"submitted"`
	Completed uint64 `json:"completed"`
	Failed    uint64 `json:"failed"`
}

// QueueStats is a queue's counts and totals, as GET /queues serves them.
type QueueStats struct {
	Counts JobCounts `json:"counts"`
	Totals JobTotals `json:"totals"`
}

// JobRecord is one job as GET /queues/{id}/jobs serves it. Times are Unix
// milliseconds, 0 when unknown.
type JobRecord struct {
	ID         string          `json:"id"`
	State      JobState        `json:"state"`
	Payload    json.RawMessage `json:"payload"`
	EnqueuedAt int64           `json:"enqueued_at,omitempty"`
	StartedAt  int64           `json:"started_at,omitempty"`
	FinishedAt int64           `json:"finished_at,omitempty"`
	Attempts   int             `json:"attempts"`
	LastError  string          `json:"last_error,omitempty"`
}

// QueueInspector is optional for storage that keeps its own job counts and
// records, such as a shared Redis queue (spec/redis-storage.md). Handler
// serves GET /queues and GET /queues/{id}/jobs from every bound queue that
// implements it; other queues are left out.
type QueueInspector interface {
	// QueueID is the queue's identity, the same on every instance that
	// binds it, so observers can group instances' answers by it.
	QueueID() string
	// QueueStats returns the queue's counts and totals.
	QueueStats() (QueueStats, error)
	// Jobs returns up to limit jobs in state, skipping the first offset.
	// Pending jobs come in the order the queue hands them out; completed
	// and failed jobs newest first.
	Jobs(state JobState, offset, limit int) ([]JobRecord, error)
}
