package varmq

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

// IRequeuer is FIFO storage that can take back items a worker dequeued in a
// batch but did not start, when the worker is stopped or paused. Only such
// storage is handed jobs in batches; a custom WithQueue storage without
// Requeue gets one job at a time. Requeue puts items back at the front, in
// order, unless stale (called under the storage's lock) reports that they
// were purged meanwhile: then it stores nothing and returns false.
type IRequeuer interface {
	Requeue(items []any, stale func() bool) bool
}

// IAcknowledgeable is the root interface of acknowledgeable operations.
type IAcknowledgeable interface {
	// Returns true if the item was successfully acknowledged, false otherwise.
	Acknowledge(ackID string) bool
	// DequeueWithAckId dequeues an item from the queue
	// Returns the item, a boolean indicating if the operation was successful, and the acknowledgment ID.
	DequeueWithAckId() (any, bool, string)
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
