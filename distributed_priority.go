package varmq

// DistributedPriorityQueue is a external queue wrapper of an any IDistributedPriorityQueue
type DistributedPriorityQueue[T any] interface {
	IExternalBaseQueue

	// Add data to the queue with priority
	Add(data T, priority int, configs ...JobConfigFunc) bool
}

type distributedPriorityQueue[T any] struct {
	internalQueue IDistributedPriorityQueue
}

func NewDistributedPriorityQueue[T any](internalQueue IDistributedPriorityQueue) DistributedPriorityQueue[T] {
	return &distributedPriorityQueue[T]{
		internalQueue: internalQueue,
	}
}

func (q *distributedPriorityQueue[T]) NumPending() int {
	return q.internalQueue.Len()
}

func (q *distributedPriorityQueue[T]) Add(data T, priority int, c ...JobConfigFunc) bool {
	j := newJob(data, loadJobConfigs(newConfig(), c...))

	jBytes, err := j.Json()

	if err != nil {
		j.Close()
		return false
	}

	if ok := q.internalQueue.Enqueue(jBytes, priority); !ok {
		j.Close()
		return false
	}

	j.setInternalQueue(q.internalQueue)

	return true
}

func (q *distributedPriorityQueue[T]) Purge() {
	q.internalQueue.Purge()
}

func (q *distributedPriorityQueue[T]) Close() error {
	return q.internalQueue.Close()
}
