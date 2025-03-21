package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
)

type Queues[T, R any] interface {
	Queue() ConcurrentQueue[T, R]
	PriorityQueue() ConcurrentPriorityQueue[T, R]
	PersistentQueue(pq types.IQueue) ConcurrentPersistentQueue[T, R]
	DistributedQueue(dq types.IQueue) DistributedQueue[T, R]
}

type queues[T, R any] struct {
	*worker[T, R]
}

func newQueues[T, R any](worker *worker[T, R]) Queues[T, R] {
	return &queues[T, R]{
		worker: worker,
	}
}

func (q *queues[T, R]) Queue() ConcurrentQueue[T, R] {
	defer q.worker.start()

	q.worker.setQueue(queue.NewQueue[job.Job[T, R]]())
	return newQueue[T, R](q.worker)
}

func (q *queues[T, R]) PriorityQueue() ConcurrentPriorityQueue[T, R] {
	defer q.worker.start()
	q.worker.setQueue(queue.NewPriorityQueue[job.Job[T, R]]())
	return newPriorityQueue[T, R](q.worker)
}

func (q *queues[T, R]) PersistentQueue(pq types.IQueue) ConcurrentPersistentQueue[T, R] {
	defer q.worker.start()
	q.worker.setQueue(pq)
	return newPersistentQueue[T, R](q.worker)
}

func (q *queues[T, R]) DistributedQueue(dq types.IQueue) DistributedQueue[T, R] {
	defer func() {
		go q.worker.listenEnqueueNotification()
	}()
	defer q.worker.start()
	q.worker.setQueue(dq)
	return NewDistributedQueue[T, R](dq)
}
