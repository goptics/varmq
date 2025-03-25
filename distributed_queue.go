package gocq

type DistributedQueue[T, R any] interface {
	PendingCount() int
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) bool
	Purge()
	Close() error
}

type distributedQueue[T, R any] struct {
	queue IDistributedQueue
}

func NewDistributedQueue[T, R any](queue IDistributedQueue) DistributedQueue[T, R] {
	return &distributedQueue[T, R]{
		queue: queue,
	}
}

func (q *distributedQueue[T, R]) PendingCount() int {
	return q.queue.Len()
}

func (q *distributedQueue[T, R]) Add(data T, c ...JobConfigFunc) bool {
	j := newJob[T, R](data, loadJobConfigs(newConfig(), c...))
	j.CloseResultChannel() // don't need result channel for distributed queue

	jBytes, err := j.Json()

	if err != nil {
		return false
	}

	return q.queue.Enqueue(jBytes)
}

func (q *distributedQueue[T, R]) Purge() {
	// TODO: send a notification to worker to remove all pending count from wait group
	q.queue.Purge()
}

func (q *distributedQueue[T, R]) Close() error {
	return q.queue.Close()
}
