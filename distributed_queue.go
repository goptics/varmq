package gocmq

type DistributedQueue[T, R any] interface {
	IExternalBaseQueue
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) bool
}

type distributedQueue[T, R any] struct {
	internalQueue IDistributedQueue
}

func NewDistributedQueue[T, R any](internalQueue IDistributedQueue) DistributedQueue[T, R] {
	return &distributedQueue[T, R]{
		internalQueue: internalQueue,
	}
}

func (q *distributedQueue[T, R]) PendingCount() int {
	return q.internalQueue.Len()
}

func (q *distributedQueue[T, R]) Add(data T, c ...JobConfigFunc) bool {
	j := newVoidJob[T, R](data, withRequiredJobId(loadJobConfigs(newConfig(), c...)))

	jBytes, err := j.Json()

	if err != nil {
		return false
	}

	return q.internalQueue.Enqueue(jBytes)
}

func (q *distributedQueue[T, R]) Purge() {
	q.internalQueue.Purge()
}

func (q *distributedQueue[T, R]) Close() error {
	return q.internalQueue.Close()
}
