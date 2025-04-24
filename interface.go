package gocq

// IWorkerQueue is the root interface of queue operations. workers queue needs to implement this interface.
type IWorkerQueue interface {
	Len() int
	Dequeue() (any, bool)
	Values() []any
	Purge()
	Close() error
}

// ISubscribable is the root interface of notifiable operations.
type ISubscribable interface {
	Subscribe(func(action string, data []byte))
}

// IAcknowledgeable is the root interface of acknowledgeable operations.
type IAcknowledgeable interface {
	// Returns true if the item was successfully acknowledged, false otherwise.
	Acknowledge(ackID string) bool
	// PrepareForFutureAck adds an item to the pending list for acknowledgment tracking
	// Returns an error if the operation fails
	PrepareForFutureAck(ackID string, item any) error
}

// IQueue is the root interface of queue operations.
type IQueue interface {
	IWorkerQueue
	Enqueue(item any) bool
}

// IPriorityQueue is the root interface of priority queue operations.
type IPriorityQueue interface {
	IWorkerQueue
	Enqueue(item any, priority int) bool
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

// IDistributedQueue is the root interface of distributed queue operations.
type IDistributedQueue interface {
	IPersistentQueue
	ISubscribable
}

type IDistributedPriorityQueue interface {
	IPersistentPriorityQueue
	ISubscribable
}

type IBaseQueue interface {
	// PendingCount returns the number of Jobs pending in the queue.
	PendingCount() int
	// Purge removes all pending Jobs from the queue.
	// Time complexity: O(n) where n is the number of pending Jobs
	Purge()
	// Close closes the queue and resets all internal states.
	// Time complexity: O(n) where n is the number of channels
	Close() error
}
