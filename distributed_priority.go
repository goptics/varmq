package varmq

// DistributedPriorityQueue is a external queue wrapper of an any IDistributedPriorityQueue
type DistributedPriorityQueue[T any] interface {
	IExternalBaseQueue

	// Add data to the queue with priority
	Add(data T, priority int, configs ...JobConfigFunc) bool
}

type distributedPriorityQueue[T any] struct {
	IDistributedPriorityQueue
	config queueConfig
}

func NewDistributedPriorityQueue[T any](internalQueue IDistributedPriorityQueue, configs ...QueueConfigFunc) DistributedPriorityQueue[T] {
	return &distributedPriorityQueue[T]{
		IDistributedPriorityQueue: internalQueue,
		config:                    loadQueueConfigs(configs...),
	}
}

func (dpq *distributedPriorityQueue[T]) NumPending() int {
	return dpq.Len()
}

func (dpq *distributedPriorityQueue[T]) IsFull() bool {
	return dpq.config.capacity > 0 && dpq.Len() >= dpq.config.capacity
}

func (dpq *distributedPriorityQueue[T]) Add(data T, priority int, c ...JobConfigFunc) bool {
	if dpq.IsFull() {
		return false
	}

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
