package varmq

import (
	"sync"

	"github.com/fahimfaisaal/varmq/internal/queues"
)

// IWorkerBinder is the base interface for binding workers to different queue types.
// It provides methods to connect workers with various queue implementations, enabling
// flexible worker-queue configurations. This interface extends the Worker interface
// with queue binding capabilities for handling tasks of type T and producing results of type R.
type IWorkerBinder[T, R any] interface {
	Worker[T, R]

	// BindQueue binds the worker to a standard Queue implementation.
	// It creates a new Queue with default settings and connects the worker to it.
	// This is the simplest way to get a standard FIFO queue working with this worker.
	//
	// Returns:
	//   - Queue[T, R]: A fully configured Queue that automatically processes jobs using this worker.
	//
	// Example usage:
	//   queue := worker.BindQueue()
	//   queue.Add(data) // Enqueues a job that will be processed by the worker
	BindQueue() Queue[T, R]

	// WithQueue binds the worker to a custom Queue implementation.
	// This method allows you to use your own queue implementation as long as it
	// satisfies the IQueue interface. This is useful when you need specialized
	// queueing behavior beyond what the standard queue provides.
	//
	// Parameters:
	//   - q IQueue: A custom queue implementation that satisfies the IQueue interface.
	//
	// Returns:
	//   - Queue[T, R]: A Queue that uses the provided implementation and processes jobs with this worker.
	//
	// Example usage:
	//   customQueue := NewCustomQueue() // satisfies IQueue interface
	//   queue := worker.WithQueue(customQueue)
	WithQueue(q IQueue) Queue[T, R]

	// BindPriorityQueue binds the worker to a standard PriorityQueue implementation.
	// It creates a new PriorityQueue with default settings and connects the worker to it.
	// Use this when you need to process jobs based on priority rather than FIFO order.
	//
	// Returns:
	//   - PriorityQueue[T, R]: A fully configured PriorityQueue that processes jobs using this worker.
	//
	// Example usage:
	//   priorityQueue := worker.BindPriorityQueue() // satisfies IPriorityQueue interface
	//   priorityQueue.Add(data, 5) // Enqueue with priority 5
	BindPriorityQueue() PriorityQueue[T, R]

	// WithPriorityQueue binds the worker to a custom PriorityQueue implementation.
	// This method allows you to use your own priority queue implementation as long as it
	// satisfies the IPriorityQueue interface. This is useful when you need specialized
	// priority-based queueing beyond what the standard implementation provides.
	//
	// Parameters:
	//   - pq IPriorityQueue: A custom priority queue implementation that satisfies the IPriorityQueue interface.
	//
	// Returns:
	//   - PriorityQueue[T, R]: A PriorityQueue that uses the provided implementation and processes jobs with this worker.
	//
	// Example usage:
	//   customPriorityQueue := NewCustomPriorityQueue()
	//   priorityQueue := worker.WithPriorityQueue(customPriorityQueue)
	WithPriorityQueue(pq IPriorityQueue) PriorityQueue[T, R]

	// WithPersistentQueue binds the worker to a PersistentQueue.
	// PersistentQueue provides durability guarantees for jobs, ensuring they are not lost
	// even in the event of application crashes or restarts. This is useful for critical
	// workloads where job completion must be guaranteed.
	//
	// Parameters:
	//   - pq IQueue: A queue implementation that provides persistence capabilities.
	//
	// Returns:
	//   - PersistentQueue[T, R]: A persistent queue that ensures jobs are not lost and processes them with the worker.
	//
	// Example usage:
	//   persistentQueue := worker.WithPersistentQueue(redisQueue)
	WithPersistentQueue(pq IPersistentQueue) PersistentQueue[T, R]

	// WithPersistentPriorityQueue binds the worker to a PersistentPriorityQueue.
	// This combines the benefits of persistence with priority-based processing. Jobs are
	// both durable and processed according to their priority values.
	//
	// Parameters:
	//   - pq IPersistentPriorityQueue: A priority queue implementation that provides persistence capabilities.
	//
	// Returns:
	//   - PersistentPriorityQueue[T, R]: A persistent priority queue that processes jobs based on priority
	//     while ensuring they are not lost, using this worker for processing.
	//
	// Example usage:
	//   persistentPriorityQueue := worker.WithPersistentPriorityQueue(persistentPriorityQueue)
	WithPersistentPriorityQueue(pq IPersistentPriorityQueue) PersistentPriorityQueue[T, R]
}

// IVoidWorkerBinder extends IWorkerBinder with distributed queue capabilities
// specifically for void workers that don't return results. This interface is specialized
// for workers that perform actions but don't need to return any data, making them suitable
// for distributed processing scenarios where results aren't needed or are handled externally.
//
// The void worker pattern is ideal for fire-and-forget operations like sending notifications,
// updating external systems, or logging events where no response is required.
type IVoidWorkerBinder[T any] interface {
	IWorkerBinder[T, any]

	// WithDistributedQueue binds the void worker to a DistributedQueue implementation.
	// Distributed queues allow job processing to be spread across multiple instances or processes,
	// enabling horizontal scaling of workers. Since void workers don't need to return results,
	// they are perfect for distributed processing scenarios.
	//
	// Parameters:
	//   - dq IDistributedQueue: A distributed queue implementation that satisfies the IDistributedQueue interface.
	//     This could be backed by Redis, RabbitMQ, Kafka, or any other distributed messaging system.
	//
	// Returns:
	//   - DistributedQueue[T, any]: A distributed queue that can distribute jobs across multiple workers or instances.
	//
	// Example usage:
	//   distributedQueue := NewDistributedQueue() // satisfies IDistributedQueue interface,  might be backed by Redis or other systems
	//   distributedQueue := voidWorker.WithDistributedQueue(distributedQueue)
	//   distributedQueue.Add(data) // This job can be processed by any worker instance listening to this queue
	WithDistributedQueue(dq IDistributedQueue) DistributedQueue[T, any]

	// WithDistributedPriorityQueue binds the void worker to a DistributedPriorityQueue implementation.
	// This combines distributed processing with priority-based job ordering. Jobs are distributed
	// across multiple instances but processed according to priority within each instance.
	// This is ideal for scenarios where some jobs are more urgent than others, but still need
	// to be processed in a distributed manner.
	//
	// Parameters:
	//   - dq IDistributedPriorityQueue: A distributed priority queue implementation that satisfies
	//     the IDistributedPriorityQueue interface. This could be backed by Redis or other
	//     distributed messaging systems that support priority-based message ordering.
	//
	// Returns:
	//   - DistributedPriorityQueue[T, any]: A distributed priority queue that can distribute jobs
	//     across multiple workers while maintaining priority ordering.
	//
	// Example usage:
	//   priorityQueue := NewDistributedPriorityQueue() // satisfies IDistributedPriorityQueue interface, might be backed by Redis or other systems
	//   distributedPriorityQueue := voidWorker.WithDistributedPriorityQueue(priorityQueue)
	//   distributedPriorityQueue.Add(data, -1) // This job will be processed with higher priority
	WithDistributedPriorityQueue(dq IDistributedPriorityQueue) DistributedPriorityQueue[T, any]
}

// workerBinder implements both IWorkerBinder and IVoidWorkerBinder interfaces
type workerBinder[T, R any] struct {
	*worker[T, R]
}

// newQueues creates a standard worker binder that implements the IWorkerBinder interface
// It wraps a worker instance and provides methods to bind it to various queue implementations
func newQueues[T, R any](worker *worker[T, R]) IWorkerBinder[T, R] {
	return &workerBinder[T, R]{
		worker: worker,
	}
}

// newVoidQueues creates a void worker binder that implements the IVoidWorkerBinder interface
// It is specifically for workers that don't return results (void workers)
// This binder adds support for distributed queue types in addition to standard queue types
func newVoidQueues[T any](worker *worker[T, any]) IVoidWorkerBinder[T] {
	// We can return the same workerBinder type but with the IVoidWorkerBinder interface
	// This works because workerBinder implements all methods of IVoidWorkerBinder
	return &workerBinder[T, any]{
		worker: worker,
	}
}

// handleQueueSubscription processes notifications from distributed queues
// When a job is enqueued in a distributed queue, this handler is called with the "enqueued" action
// It then notifies the worker to pull and process the new job
func (qs *workerBinder[T, R]) handleQueueSubscription(action string) {
	switch action {
	case "enqueued":
		qs.worker.notifyToPullNextJobs()
	}
}

// BindQueue creates and binds a new standard queue to the worker
// It returns a Queue interface that can be used to add jobs to the queue
func (qs *workerBinder[T, R]) BindQueue() Queue[T, R] {
	return qs.WithQueue(queues.NewQueue[iJob[T, R]]())
}

// WithQueue binds an existing queue implementation to the worker
// It starts the worker and returns a Queue interface to interact with the queue
func (qs *workerBinder[T, R]) WithQueue(q IQueue) Queue[T, R] {
	qs.worker.start()

	return newQueue(qs.worker, q)
}

func (q *workerBinder[T, R]) BindPriorityQueue() PriorityQueue[T, R] {
	return q.WithPriorityQueue(queues.NewPriorityQueue[iJob[T, R]]())
}

func (q *workerBinder[T, R]) WithPriorityQueue(pq IPriorityQueue) PriorityQueue[T, R] {
	q.worker.start()

	return newPriorityQueue(q.worker, pq)
}

func (q *workerBinder[T, R]) WithPersistentQueue(pq IPersistentQueue) PersistentQueue[T, R] {
	q.worker.start()
	// if cache is not set, use sync.Map as the default cache, we need it for persistent queue
	if q.worker.isNullCache() {
		q.setCache(new(sync.Map))
	}

	return newPersistentQueue(q.worker, pq)
}

func (q *workerBinder[T, R]) WithPersistentPriorityQueue(pq IPersistentPriorityQueue) PersistentPriorityQueue[T, R] {
	q.worker.start()
	// if cache is not set, use sync.Map as the default cache, we need it for persistent queue
	if q.worker.isNullCache() {
		q.setCache(new(sync.Map))
	}

	return newPersistentPriorityQueue(q.worker, pq)
}

func (qs *workerBinder[T, R]) WithDistributedQueue(dq IDistributedQueue) DistributedQueue[T, R] {
	qs.worker.start()
	defer dq.Subscribe(qs.handleQueueSubscription)

	queue := NewDistributedQueue[T, R](dq)
	qs.worker.setQueue(dq)
	return queue
}

func (qs *workerBinder[T, R]) WithDistributedPriorityQueue(dq IDistributedPriorityQueue) DistributedPriorityQueue[T, R] {
	qs.worker.start()
	defer dq.Subscribe(qs.handleQueueSubscription)
	defer qs.worker.setQueue(dq)

	return NewDistributedPriorityQueue[T, R](dq)
}
