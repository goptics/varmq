package gocq

import (
	"sync"

	"github.com/fahimfaisaal/gocq/v3/internal/queues"
)

// IWorkerBinder is the base interface for binding workers to different queue types
type IWorkerBinder[T, R any] interface {
	Worker[T, R]
	// BindQueue binds the worker to a Queue.
	BindQueue() Queue[T, R]
	// BindWithQueue binds the worker to the provided Queue.
	BindWithQueue(q IQueue) Queue[T, R]
	// BindPriorityQueue binds the worker to a PriorityQueue.
	BindPriorityQueue() PriorityQueue[T, R]
	// BindWithPriorityQueue binds the worker to the provided PriorityQueue.
	BindWithPriorityQueue(pq IPriorityQueue) PriorityQueue[T, R]
	// BindWithPersistentQueue binds the worker to a PersistentQueue.
	BindWithPersistentQueue(pq IQueue) PersistentQueue[T, R]
	// BindWithPersistentPriorityQueue binds the worker to a PersistentPriorityQueue.
	BindWithPersistentPriorityQueue(pq IPriorityQueue) PersistentPriorityQueue[T, R]
}

// IVoidWorkerBinder extends IWorkerBinder with distributed queue capabilities
// specifically for void workers that don't return results
type IVoidWorkerBinder[T any] interface {
	IWorkerBinder[T, any]
	// BindWithDistributedQueue binds the worker to a DistributedQueue.
	BindWithDistributedQueue(dq IDistributedQueue) DistributedQueue[T, any]
	// BindWithDistributedPriorityQueue binds the worker to a DistributedPriorityQueue.
	BindWithDistributedPriorityQueue(dq IDistributedPriorityQueue) DistributedPriorityQueue[T, any]
}

// workerBinder implements both IWorkerBinder and IVoidWorkerBinder interfaces
type workerBinder[T, R any] struct {
	*worker[T, R]
}

// Creates a standard worker binder
func newQueues[T, R any](worker *worker[T, R]) IWorkerBinder[T, R] {
	return &workerBinder[T, R]{
		worker: worker,
	}
}

// Creates a void worker binder with distributed queue support
func newVoidQueues[T any](worker *worker[T, any]) IVoidWorkerBinder[T] {
	// We can return the same workerBinder type but with the IVoidWorkerBinder interface
	// This works because workerBinder implements all methods of IVoidWorkerBinder
	return &workerBinder[T, any]{
		worker: worker,
	}
}

func (qs *workerBinder[T, R]) handleQueueSubscription(action string, _ []byte) {
	switch action {
	case "enqueued":
		qs.worker.notifyToPullNextJobs()
	}
}

func (qs *workerBinder[T, R]) BindQueue() Queue[T, R] {
	return qs.BindWithQueue(queues.NewQueue[iJob[T, R]]())
}

func (qs *workerBinder[T, R]) BindWithQueue(q IQueue) Queue[T, R] {
	qs.worker.start()

	return newQueue(qs.worker, q)
}

func (q *workerBinder[T, R]) BindPriorityQueue() PriorityQueue[T, R] {
	return q.BindWithPriorityQueue(queues.NewPriorityQueue[iJob[T, R]]())
}

func (q *workerBinder[T, R]) BindWithPriorityQueue(pq IPriorityQueue) PriorityQueue[T, R] {
	q.worker.start()

	return newPriorityQueue(q.worker, pq)
}

func (q *workerBinder[T, R]) BindWithPersistentQueue(pq IQueue) PersistentQueue[T, R] {
	q.worker.start()
	// if cache is not set, use sync.Map as the default cache, we need it for persistent queue
	if q.worker.isNullCache() {
		q.setCache(new(sync.Map))
	}

	return newPersistentQueue(q.worker, pq)
}

func (q *workerBinder[T, R]) BindWithPersistentPriorityQueue(pq IPriorityQueue) PersistentPriorityQueue[T, R] {
	q.worker.start()
	// if cache is not set, use sync.Map as the default cache, we need it for persistent queue
	if q.worker.isNullCache() {
		q.setCache(new(sync.Map))
	}

	return newPersistentPriorityQueue(q.worker, pq)
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
