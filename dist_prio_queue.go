package gocq

type DistributedPriorityQueue[T, R any] interface {
	IBaseQueue
	// Time complexity: O(log n)
	Add(data T, priority int, configs ...JobConfigFunc) bool
}

type distributedPriorityQueue[T, R any] struct {
	queue IDistributedPriorityQueue
}

func NewDistributedPriorityQueue[T, R any](queue IDistributedPriorityQueue) DistributedPriorityQueue[T, R] {
	return &distributedPriorityQueue[T, R]{
		queue: queue,
	}
}

func (q *distributedPriorityQueue[T, R]) PendingCount() int {
	return q.queue.Len()
}

func (q *distributedPriorityQueue[T, R]) Add(data T, priority int, c ...JobConfigFunc) bool {
	j := newJob[T, R](data, loadJobConfigs(newConfig(), c...))
	j.CloseResultChannel() // don't need result channel for distributed queue

	jBytes, err := j.Json()

	if err != nil {
		return false
	}

	return q.queue.Enqueue(jBytes, priority)
}

func (q *distributedPriorityQueue[T, R]) Purge() {
	q.queue.Purge()
}

func (q *distributedPriorityQueue[T, R]) Close() error {
	return q.queue.Close()
}
