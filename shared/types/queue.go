package types

type IQueue interface {
	Len() int
	Dequeue() (any, bool)
	Enqueue(item any) bool
	Values() []any
	Close() error
}

type IDistributedQueue interface {
	IQueue
	NotificationChannel() <-chan struct{}
}
