package gocq

import (
	"sync"

	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
)

type Queues[T, R any] interface {
	Worker[T, R]
	// BindQueue binds the worker to a ConcurrentQueue.
	BindQueue() ConcurrentQueue[T, R]
	// BindPriorityQueue binds the worker to a ConcurrentPriorityQueue.
	BindPriorityQueue() ConcurrentPriorityQueue[T, R]
	// BindWithPersistentQueue binds the worker to a ConcurrentPersistentQueue.
	BindWithPersistentQueue(pq types.IQueue) ConcurrentPersistentQueue[T, R]
	// BindWithDistributedQueue binds the worker to a DistributedQueue.
	BindWithDistributedQueue(dq types.IDistributedQueue) DistributedQueue[T, R]
}

type queues[T, R any] struct {
	*worker[T, R]
}

func newQueues[T, R any](worker *worker[T, R]) Queues[T, R] {
	return &queues[T, R]{
		worker: worker,
	}
}

func (qs *queues[T, R]) isBound() {
	if qs.Queue != queue.GetNullQueue() {
		panic(errWorkerAlreadyBound)
	}
}

func (qs *queues[T, R]) BindQueue() ConcurrentQueue[T, R] {
	qs.isBound()
	defer qs.worker.start()

	return newQueue[T, R](qs.worker)
}

func (q *queues[T, R]) BindPriorityQueue() ConcurrentPriorityQueue[T, R] {
	q.isBound()
	defer q.worker.start()

	return newPriorityQueue[T, R](q.worker)
}

func (q *queues[T, R]) BindWithPersistentQueue(pq types.IQueue) ConcurrentPersistentQueue[T, R] {
	q.isBound()
	defer q.worker.start()
	// if cache is not set, use sync.Map as the default cache, we need it for persistent queue
	if q.worker.isNullCache() {
		q.worker.Cache = new(sync.Map)
	}

	return newPersistentQueue[T, R](q.worker, pq)
}

func (q *queues[T, R]) BindWithDistributedQueue(dq types.IDistributedQueue) DistributedQueue[T, R] {
	q.isBound()
	queue := NewDistributedQueue[T, R](dq)

	defer func() {
		go queue.listenEnqueueNotification(func() {
			q.worker.sync.wg.Add(1)
			q.worker.jobPullNotifier.Notify()
		})
	}()
	defer q.worker.start()

	q.worker.setQueue(dq)
	return queue
}
