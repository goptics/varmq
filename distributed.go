package varmq

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

func (q *distributedQueue[T, R]) NumPending() int {
	return q.internalQueue.Len()
}

func (q *distributedQueue[T, R]) Add(data T, c ...JobConfigFunc) bool {
	j := newVoidJob[T, R](data, withRequiredJobId(loadJobConfigs(newConfig(), c...)))

	jBytes, err := j.Json()

	if err != nil {
		j.close()

		return false
	}

	if ok := q.internalQueue.Enqueue(jBytes); !ok {
		j.close()
		return false
	}

	j.SetInternalQueue(q.internalQueue)

	return true
}

func (q *distributedQueue[T, R]) Purge() {
	q.internalQueue.Purge()
}

func (q *distributedQueue[T, R]) Close() error {
	return q.internalQueue.Close()
}
