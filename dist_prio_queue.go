package gocmq

type DistributedPriorityQueue[T, R any] interface {
	IExternalBaseQueue
	// Time complexity: O(log n)
	Add(data T, priority int, configs ...JobConfigFunc) bool
}

type distributedPriorityQueue[T, R any] struct {
	internalQueue IDistributedPriorityQueue
}

func NewDistributedPriorityQueue[T, R any](internalQueue IDistributedPriorityQueue) DistributedPriorityQueue[T, R] {
	return &distributedPriorityQueue[T, R]{
		internalQueue: internalQueue,
	}
}

func (q *distributedPriorityQueue[T, R]) PendingCount() int {
	return q.internalQueue.Len()
}

func (q *distributedPriorityQueue[T, R]) Add(data T, priority int, c ...JobConfigFunc) bool {
	j := newVoidJob[T, R](data, withRequiredJobId(loadJobConfigs(newConfig(), c...)))

	jBytes, err := j.Json()

	if err != nil {
		return false
	}

	return q.internalQueue.Enqueue(jBytes, priority)
}

func (q *distributedPriorityQueue[T, R]) Purge() {
	q.internalQueue.Purge()
}

func (q *distributedPriorityQueue[T, R]) Close() error {
	return q.internalQueue.Close()
}
