package varmq

// DistributedPriorityQueue is a external queue wrapper of an any IDistributedPriorityQueue
type DistributedPriorityQueue[T any] interface {
	IExternalBaseQueue

	// Add data to the queue with priority
	Add(data T, priority int, configs ...JobConfigFunc) bool
}

type distributedPriorityQueue[T any] struct {
	IDistributedPriorityQueue
}

func NewDistributedPriorityQueue[T any](internalQueue IDistributedPriorityQueue) DistributedPriorityQueue[T] {
	return &distributedPriorityQueue[T]{
		IDistributedPriorityQueue: internalQueue,
	}
}

func (dpq *distributedPriorityQueue[T]) NumPending() int {
	return dpq.Len()
}

func (dpq *distributedPriorityQueue[T]) Add(data T, priority int, c ...JobConfigFunc) bool {
	j := newJob(data, loadJobConfigs(newConfig(), c...))

	jBytes, err := j.Json()

	if err != nil {
		j.Close()
		return false
	}

	if ok := dpq.Enqueue(jBytes, priority); !ok {
		j.Close()
		return false
	}

	return true
}
