package varmq

// DistributedQueue is a external queue wrapper of an any IDistributedQueue
type DistributedQueue[T any] interface {
	IExternalBaseQueue
	// Add data to the queue
	Add(data T, configs ...JobConfigFunc) bool
}

type distributedQueue[T any] struct {
	IDistributedQueue
}

func NewDistributedQueue[T any](internalQueue IDistributedQueue) DistributedQueue[T] {
	return &distributedQueue[T]{
		IDistributedQueue: internalQueue,
	}
}

func (dq *distributedQueue[T]) NumPending() int {
	return dq.Len()
}

func (dq *distributedQueue[T]) Add(data T, c ...JobConfigFunc) bool {
	j := newJob(data, loadJobConfigs(newConfig(), c...))

	jBytes, err := j.Json()

	if err != nil {
		j.Close()
		return false
	}

	if ok := dq.Enqueue(jBytes); !ok {
		j.Close()
		return false
	}

	return true
}
