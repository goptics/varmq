package queue

type EnqItem[T any] struct {
	Value    T
	Priority int
	Index    int
}
