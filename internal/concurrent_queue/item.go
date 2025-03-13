package concurrent_queue

type PQItem[T any] struct {
	Value    T
	Priority int
}
