package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
)

type DistributedQueue[T, R any] interface {
	// Time complexity: O(1)
	Add(data T, id ...string) bool
}

type distributedQueue[T, R any] struct {
	queue types.IQueue
}

func NewDistributedQueue[T, R any](queue types.IQueue) DistributedQueue[T, R] {
	return &distributedQueue[T, R]{
		queue: queue,
	}
}

func (q *distributedQueue[T, R]) Add(data T, id ...string) bool {
	j := job.New[T, R](data, id...)

	return q.queue.Enqueue(queue.EnqItem[job.Job[T, R]]{Value: j})
}
