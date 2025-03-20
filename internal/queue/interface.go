package queue

type IQueue interface {
	Dequeue() (any, bool)
	Enqueue(item any)
	Init()
	Len() int
	Values() []any
}
