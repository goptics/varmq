package varmq

import (
	"errors"

	"github.com/goptics/varmq/contract"
)

// IBaseQueue is the part every queue storage implements.
type IBaseQueue interface {
	// Len returns how many items wait in the queue.
	Len() int
	// Dequeue removes and returns the next item; false when empty.
	Dequeue() (any, bool)
	// Values returns the waiting items, front first.
	Values() []any
	// Purge removes every waiting item.
	Purge()
	// Close releases the storage.
	Close() error
}

// IQueue is FIFO storage.
type IQueue interface {
	IBaseQueue
	// Enqueue stores item; false when refused.
	Enqueue(item any) bool
	// EnqueueOnce stores item unless one with the same id is stored. Storage
	// that does not dedupe can call Enqueue. Persistent storage keeps an id for
	// a window and returns true for a repeat; in-memory storage keeps it while
	// the item is queued and returns false for a repeat.
	EnqueueOnce(item any, id string) bool
}

// IPriorityQueue is storage that hands out the lowest priority number first.
type IPriorityQueue interface {
	IBaseQueue
	// Enqueue stores item with priority; false when refused.
	Enqueue(item any, priority int) bool
	// EnqueueOnce is IQueue.EnqueueOnce with a priority.
	EnqueueOnce(item any, priority int, id string) bool
}

// enqueue stores item in q, once per id when the job has one.
func enqueue(q IQueue, item any, id string) bool {
	if id != "" {
		return q.EnqueueOnce(item, id)
	}
	return q.Enqueue(item)
}

// enqueuePriority is enqueue for priority storage.
func enqueuePriority(q IPriorityQueue, item any, priority int, id string) bool {
	if id != "" {
		return q.EnqueueOnce(item, priority, id)
	}
	return q.Enqueue(item, priority)
}

// IAcknowledgeable is storage that holds a dequeued item until the worker
// reports its outcome: Acknowledge on success, Reject on its final failure.
// varmq has no retries, so every failure is final. See spec/redis-storage.md.
type IAcknowledgeable interface {
	// Acknowledge marks the item held under ackID completed; false when
	// storage could not record it.
	Acknowledge(ackID string) bool
	// Reject marks the item held under ackID failed for good, without
	// requeueing it. reason is the error text. False when storage could not
	// record it.
	Reject(ackID string, reason string) bool
	// DequeueWithAckId returns the next item and its ack id; false when empty.
	DequeueWithAckId() (any, bool, string)
}

// IBatchAcknowledgeable is optional for acknowledgeable storage: a worker whose
// only source implements it pops one batch per dispatch.
type IBatchAcknowledgeable interface {
	IAcknowledgeable
	// DequeueBatchWithAckIds atomically moves up to min(len(items),
	// len(ackIDs)) items to pending, into items[:n] and ackIDs[:n]. remaining
	// is the length left (or a lower bound), above 0 while items remain. As a
	// distributed queue the worker waits for "enqueued" after a short or failed
	// pop, so storage must also announce items no Enqueue did (e.g. requeues).
	DequeueBatchWithAckIds(items []any, ackIDs []string) (n, remaining int, err error)
}

// IPersistentQueue is FIFO storage with acknowledgement.
type IPersistentQueue interface {
	IQueue
	IAcknowledgeable
}

// IPersistentPriorityQueue is priority storage with acknowledgement.
type IPersistentPriorityQueue interface {
	IPriorityQueue
	IAcknowledgeable
}

// ISubscribable is storage that notifies of changes, such as "enqueued".
type ISubscribable interface {
	// Subscribe registers fn to receive each change action.
	Subscribe(fn func(action string))
}

// IDistributedQueue is persistent FIFO storage shared across processes.
type IDistributedQueue interface {
	IPersistentQueue
	ISubscribable
}

// IDistributedPriorityQueue is persistent priority storage shared across
// processes.
type IDistributedPriorityQueue interface {
	IPersistentPriorityQueue
	ISubscribable
}

// CapacitySetter is optional: it receives the WithQueueCapacity limit, 0 for
// none.
type CapacitySetter interface {
	// SetCapacity sets the most items the storage accepts.
	SetCapacity(cap int)
}

// IPurgeValuesQueue is optional for any storage. A queue's Purge closes the
// jobs PurgeValues returns, so PurgeValues must remove every stored item and
// return them in one atomic step: an item added meanwhile is either returned
// or stays stored. Without it, Purge calls Values and then Purge, and an item
// added between the two is removed without being closed, so its Wait never
// returns. In-memory storage (BindQueue, BindPriorityQueue) implements it.
type IPurgeValuesQueue interface {
	PurgeValues() []any
}

// QueuePauser is optional for any storage: a durable queue pause, kept in
// the storage and seen by every process that reads it. While paused the
// storage's Dequeue (and DequeueWithAckId, DequeueBatchWithAckIds) hands
// out nothing; adds still land. Pause and Resume are idempotent and return
// once the flag is stored. Paused is a local read: O(1), no I/O. A dequeue
// that finds the flag set must make Paused true before it returns and, for
// a batch pop, report remaining 0, so a stale Paused costs one empty pop,
// never a spin. See spec/redis-storage.md and adr 0004.
type QueuePauser interface {
	Pause() error
	Resume() error
	Paused() bool
}

// PauseNotifier is optional for a QueuePauser: fn runs on every change of
// Paused, from this process or another. A bound worker uses it to start at
// once on a Resume; without it a Resume waits for the next add or wake-up.
// cancel stops the calls.
type PauseNotifier interface {
	OnPauseChange(fn func(paused bool)) (cancel func())
}

// ErrPauseUnsupported is returned by a queue's Pause and Resume when its
// storage is not a QueuePauser.
var ErrPauseUnsupported = errors.New("varmq: storage does not support pause")

// The types observers read through Handler come from the contract module,
// the Go copy of spec/ that storage such as redisq imports without varmq.
type (
	JobState       = contract.JobState
	JobCounts      = contract.JobCounts
	JobTotals      = contract.JobTotals
	QueueStats     = contract.QueueStats
	JobRecord      = contract.JobRecord
	JobError       = contract.JobError
	QueueInspector = contract.QueueInspector

	InstanceRecord   = contract.InstanceRecord
	InstanceWorker   = contract.InstanceWorker
	InstanceRegistry = contract.InstanceRegistry
)

const (
	JobPending    = contract.JobPending
	JobProcessing = contract.JobProcessing
	JobCompleted  = contract.JobCompleted
	JobFailed     = contract.JobFailed
)
