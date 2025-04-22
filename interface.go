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

// INotifiable is the root interface of notifiable operations.
type INotifiable interface {
	Notify(action string, data []byte)
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
	INotifiable
}

// IPersistentPriorityQueue is the root interface of persistent priority queue operations.
type IPersistentPriorityQueue interface {
	IPriorityQueue
	INotifiable
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
