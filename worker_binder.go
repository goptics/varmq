package gocq

import (
	"sync"
)

type IWorkerBinder[T, R any] interface {
	Worker[T, R]
	// BindQueue binds the worker to a ConcurrentQueue.
	BindQueue() ConcurrentQueue[T, R]
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

func (qs *workerBinder[T, R]) isBound() {
	if qs.Queue != getNullQueue() {
		panic(errWorkerAlreadyBound)
	}
}

func (qs *workerBinder[T, R]) BindQueue() ConcurrentQueue[T, R] {
	qs.isBound()
	defer qs.worker.start()

	return newQueue[T, R](qs.worker)
}

func (q *workerBinder[T, R]) BindPriorityQueue() ConcurrentPriorityQueue[T, R] {
	q.isBound()
	defer q.worker.start()

	return newPriorityQueue[T, R](q.worker)
}

func (q *workerBinder[T, R]) BindWithPersistentQueue(pq IQueue) ConcurrentPersistentQueue[T, R] {
	q.isBound()
	defer q.worker.start()
	// if cache is not set, use sync.Map as the default cache, we need it for persistent queue
	if q.worker.isNullCache() {
		q.worker.Cache = new(sync.Map)
	}

	return newPersistentQueue[T, R](q.worker, pq)
}

func (q *workerBinder[T, R]) BindWithDistributedQueue(dq IDistributedQueue) DistributedQueue[T, R] {
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
