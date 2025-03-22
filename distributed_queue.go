package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
)

type DistributedQueue[T, R any] interface {
	PendingCount() int
	// Time complexity: O(1)
	Add(data T, id ...string) bool
	Close() error
	listenEnqueueNotification(func())
}

type distributedQueue[T, R any] struct {
	queue types.IDistributedQueue
}

func NewDistributedQueue[T, R any](queue types.IDistributedQueue) DistributedQueue[T, R] {
	return &distributedQueue[T, R]{
		queue: queue,
	}
}

func (dq *distributedQueue[T, R]) listenEnqueueNotification(fn func()) {
	for range dq.queue.NotificationChannel() {
		fn()
	}
}

func (q *distributedQueue[T, R]) PendingCount() int {
	return q.queue.Len()
}

func (q *distributedQueue[T, R]) Add(data T, id ...string) bool {
	j := job.New[T, R](data, id...)
	j.CloseResultChannel() // don't need result channel for distributed queue

	jBytes, err := j.Json()

	if err != nil {
		return false
	}

	return q.queue.Enqueue(jBytes)
}

func (q *distributedQueue[T, R]) Close() error {
	return q.queue.Close()
}
