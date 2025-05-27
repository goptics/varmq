package varmq

// DistributedQueue is a external queue wrapper of an any IDistributedQueue
type DistributedQueue[T any] interface {
	IExternalBaseQueue
	// Add data to the queue
	Add(data T, configs ...JobConfigFunc) bool
}

type distributedQueue[T any] struct {
	internalQueue IDistributedQueue
}

func NewDistributedQueue[T any](internalQueue IDistributedQueue) DistributedQueue[T] {
	return &distributedQueue[T]{
		internalQueue: internalQueue,
	}
}

func (q *distributedQueue[T]) NumPending() int {
	return q.internalQueue.Len()
}

func (q *distributedQueue[T]) Add(data T, c ...JobConfigFunc) bool {
	j := newJob(data, loadJobConfigs(newConfig(), c...))

	jBytes, err := j.Json()

	if err != nil {
		j.Close()

		return false
	}

	if ok := q.internalQueue.Enqueue(jBytes); !ok {
		j.Close()
		return false
	}

	j.setInternalQueue(q.internalQueue)

	return true
}

func (q *distributedQueue[T]) Purge() {
	q.internalQueue.Purge()
}

func (q *distributedQueue[T]) Close() error {
	return q.internalQueue.Close()
}
