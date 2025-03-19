package queue

type IQueue[T any] interface {
	Dequeue() (any, bool)
	Enqueue(item any)
	Init()
	Len() int
	Values() []T
}
