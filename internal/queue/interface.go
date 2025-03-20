package queue

type IQueue interface {
	Dequeue() (any, bool)
	Enqueue(item any) bool
	Init()
	Len() int
	Values() []any
	Close() error
}
