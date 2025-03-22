package types

type IWorkerQueue interface {
	Len() int
	Dequeue() (any, bool)
	Values() []any
	Close() error
}

type IQueue interface {
	IWorkerQueue
	Enqueue(item any) bool
}

type IPriorityQueue interface {
	IWorkerQueue
	Enqueue(item any, priority int) bool
}

type DistributedQueue interface {
	NotificationChannel() <-chan struct{}
}

type IDistributedQueue interface {
	IQueue
	DistributedQueue
}

type IDistributedPriorityQueue interface {
	IPriorityQueue
	DistributedQueue
}
