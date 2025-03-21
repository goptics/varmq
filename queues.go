package gocq

type Queues[T, R any] interface {
	Queue() ConcurrentQueue[T, R]
	PriorityQueue() ConcurrentPriorityQueue[T, R]
	PersistentQueue() ConcurrentPersistentQueue[T, R]
}

type queues[T, R any] struct {
	worker Worker[T, R]
}

func newQueues[T, R any](worker Worker[T, R]) Queues[T, R] {
	return &queues[T, R]{
		worker: worker,
	}
}

func (q *queues[T, R]) Queue() ConcurrentQueue[T, R] {
	defer q.worker.Start()
	return newQueue[T, R](q.worker)
}

func (q *queues[T, R]) PriorityQueue() ConcurrentPriorityQueue[T, R] {
	defer q.worker.Start()
	return newPriorityQueue[T, R](q.worker)
}

func (q *queues[T, R]) PersistentQueue() ConcurrentPersistentQueue[T, R] {
	defer q.worker.Start()
	return newPersistentQueue[T, R](q.worker)
}
