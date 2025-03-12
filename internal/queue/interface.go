package queue

type IQueue[T any] interface {
	Dequeue() (T, bool)
	Enqueue(item EnqItem[T])
	Init()
	Len() int
	Values() []T
}
