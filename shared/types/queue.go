package types

type IQueue interface {
	NotificationChannel() <-chan struct{}
	Dequeue() (any, bool)
	Enqueue(item any) bool
	Init()
	Len() int
	Values() []any
	Close() error
}
