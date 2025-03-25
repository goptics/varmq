package gocq

import (
	"sync"
)

type IWorkerBinder[T, R any] interface {
	Worker[T, R]
	// BindQueue binds the worker to a Queue.
	BindQueue() Queue[T, R]
	// BindPriorityQueue binds the worker to a ConcurrentPriorityQueue.
	BindPriorityQueue() ConcurrentPriorityQueue[T, R]
	// BindWithPersistentQueue binds the worker to a ConcurrentPersistentQueue.
	BindWithPersistentQueue(pq IQueue) ConcurrentPersistentQueue[T, R]
	// BindWithDistributedQueue binds the worker to a DistributedQueue.
	BindWithDistributedQueue(dq IDistributedQueue) DistributedQueue[T, R]
}

type workerBinder[T, R any] struct {
	*worker[T, R]
}

func newQueues[T, R any](worker *worker[T, R]) IWorkerBinder[T, R] {
	return &workerBinder[T, R]{
		worker: worker,
	}
}

func (qs *workerBinder[T, R]) handleQueueSubscription(action string, data []byte) {
	switch action {
	case "enqueued":
		qs.worker.notifyToPullJobs()
	}
}

func (qs *workerBinder[T, R]) BindQueue() Queue[T, R] {
	defer qs.worker.start()

	return newQueue[T, R](qs.worker)
}

func (q *workerBinder[T, R]) BindPriorityQueue() ConcurrentPriorityQueue[T, R] {
	defer q.worker.start()

	return newPriorityQueue[T, R](q.worker)
}

func (q *workerBinder[T, R]) BindWithPersistentQueue(pq IQueue) ConcurrentPersistentQueue[T, R] {
	defer q.worker.start()
	// if cache is not set, use sync.Map as the default cache, we need it for persistent queue
	if q.worker.isNullCache() {
		q.setCache(new(sync.Map))
	}

	return newPersistentQueue[T, R](q.worker, pq)
}

func (qs *workerBinder[T, R]) BindWithDistributedQueue(dq IDistributedQueue) DistributedQueue[T, R] {
	qs.worker.start()
	defer dq.Subscribe(qs.handleQueueSubscription)

	queue := NewDistributedQueue[T, R](dq)
	qs.worker.setQueue(dq)
	return queue
}

func (qs *workerBinder[T, R]) BindWithDistributedPriorityQueue(dq IDistributedPriorityQueue) DistributedPriorityQueue[T, R] {
	qs.worker.start()
	defer dq.Subscribe(qs.handleQueueSubscription)
	defer qs.worker.setQueue(dq)

	return NewDistributedPriorityQueue[T, R](dq)
}
