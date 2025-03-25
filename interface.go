package gocq

// IWorkerQueue is the root interface of queue operations. workers queue needs to implement this interface.
type IWorkerQueue interface {
	Len() int
	Dequeue() (any, bool)
	Values() []any
	Purge()
	Close() error
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

// ISubsctibable is the root interface of notifiable operations.
type ISubscribable interface {
	Subscribe(func(action string, data []byte))
}

// IDistributedQueue is the root interface of distributed queue operations.
type IDistributedQueue interface {
	IQueue
	ISubscribable
}

type IDistributedPriorityQueue interface {
	IPriorityQueue
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

// ICQueue is the root interface of concurrent queue operations.
type ICQueue[T, R any] interface {
	IBaseQueue
	Worker() Worker[T, R]
	// JobById returns the job with the given id.
	JobById(id string) (EnqueuedJob[R], error)
	// GroupsJobById returns the groups job with the given id.
	GroupsJobById(id string) (EnqueuedSingleGroupJob[R], error)
	// WaitUntilFinished waits until all pending Jobs in the queue are processed.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitUntilFinished()
	// WaitAndClose waits until all pending Jobs in the queue are processed and then closes the queue.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitAndClose() error
}
