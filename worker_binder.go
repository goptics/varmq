package varmq

import (
	"github.com/goptics/varmq/internal/collections"
)

// IWorkerBinder is the base interface for binding workers to different queue types.
// It provides methods to connect workers with various queue implementations, enabling
// flexible worker-queue configurations. This interface extends the Worker interface
// with queue binding capabilities for handling tasks of type T and producing results of type R.
type IResultWorkerBinder[T, R any] interface {
	Worker

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
	BindQueue() ResultQueue[T, R]

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
	WithQueue(q IQueue) ResultQueue[T, R]

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
	BindPriorityQueue() ResultPriorityQueue[T, R]

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
	WithPriorityQueue(pq IPriorityQueue) ResultPriorityQueue[T, R]
}

type IWorkerBinder[T any] interface {
	Worker
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
	BindQueue() Queue[T]

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
	WithQueue(q IQueue) Queue[T]

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
	BindPriorityQueue() PriorityQueue[T]

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
	WithPriorityQueue(pq IPriorityQueue) PriorityQueue[T]

	// WithPersistentQueue binds the worker to a PersistentQueue.
	// PersistentQueue provides durability guarantees for jobs, ensuring they are not lost
	// even in the event of application crashes or restarts. This is useful for critical
	// workloads where job completion must be guaranteed.
	//
	// Parameters:
	//   - pq IQueue: A queue implementation that provides persistence capabilities.
	//
	// Returns:
	//   - PersistentQueue[T, any]: A persistent queue that ensures jobs are not lost and processes them with the worker.
	//
	// Example usage:
	//   persistentQueue := worker.WithPersistentQueue(redisQueue)
	WithPersistentQueue(pq IPersistentQueue) PersistentQueue[T]

	// WithPersistentPriorityQueue binds the worker to a PersistentPriorityQueue.
	// This combines the benefits of persistence with priority-based processing. Jobs are
	// both durable and processed according to their priority values.
	//
	// Parameters:
	//   - pq IPersistentPriorityQueue: A priority queue implementation that provides persistence capabilities.
	//
	// Returns:
	//   - PersistentPriorityQueue[T, any]: A persistent priority queue that processes jobs based on priority
	//     while ensuring they are not lost, using this worker for processing.
	//
	// Example usage:
	//   persistentPriorityQueue := worker.WithPersistentPriorityQueue(persistentPriorityQueue)
	WithPersistentPriorityQueue(pq IPersistentPriorityQueue) PersistentPriorityQueue[T]

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
	WithDistributedQueue(dq IDistributedQueue) DistributedQueue[T]

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
	WithDistributedPriorityQueue(dq IDistributedPriorityQueue) DistributedPriorityQueue[T]
}

// workerBinder implements both IWorkerBinder and IVoidWorkerBinder interfaces
type workerBinder[T any] struct {
	*worker[T, iJob[T]]
}

// newVoidQueues creates a void worker binder that implements the IVoidWorkerBinder interface
// It is specifically for workers that don't return results (void workers)
// This binder adds support for distributed queue types in addition to standard queue types
func newQueues[T any](worker *worker[T, iJob[T]]) IWorkerBinder[T] {
	// We can return the same workerBinder type but with the IVoidWorkerBinder interface
	// This works because workerBinder implements all methods of IVoidWorkerBinder
	return &workerBinder[T]{
		worker: worker,
	}
}

// handleQueueSubscription processes notifications from distributed queues
// When a job is enqueued in a distributed queue, this handler is called with the "enqueued" action
// It then notifies the worker to pull and process the new job
func (wb *workerBinder[T]) handleQueueSubscription(action string) {
	switch action {
	case "enqueued":
		wb.worker.notifyToPullNextJobs()
	}
}

// BindQueue creates and binds a new standard queue to the worker
// It returns a Queue interface that can be used to add jobs to the queue
func (wb *workerBinder[T]) BindQueue() Queue[T] {
	return wb.WithQueue(collections.NewQueue[iJob[T]]())
}

// WithQueue binds an existing queue implementation to the worker
// It starts the worker and returns a Queue interface to interact with the queue
func (wb *workerBinder[T]) WithQueue(q IQueue) Queue[T] {
	wb.worker.start()

	return newQueue(wb.worker, q)
}

func (wb *workerBinder[T]) BindPriorityQueue() PriorityQueue[T] {
	return wb.WithPriorityQueue(collections.NewPriorityQueue[iJob[T]]())
}

func (wb *workerBinder[T]) WithPriorityQueue(pq IPriorityQueue) PriorityQueue[T] {
	wb.worker.start()

	return newPriorityQueue(wb.worker, pq)
}

func (wb *workerBinder[T]) WithPersistentQueue(pq IPersistentQueue) PersistentQueue[T] {
	wb.worker.start()

	return newPersistentQueue(wb.worker, pq)
}

func (wb *workerBinder[T]) WithPersistentPriorityQueue(pq IPersistentPriorityQueue) PersistentPriorityQueue[T] {
	wb.worker.start()

	return newPersistentPriorityQueue(wb.worker, pq)
}

func (wb *workerBinder[T]) WithDistributedQueue(dq IDistributedQueue) DistributedQueue[T] {
	wb.worker.start()
	defer dq.Subscribe(wb.handleQueueSubscription)

	queue := NewDistributedQueue[T](dq)
	wb.worker.setQueue(dq)
	return queue
}

func (wb *workerBinder[T]) WithDistributedPriorityQueue(dq IDistributedPriorityQueue) DistributedPriorityQueue[T] {
	wb.worker.start()
	defer dq.Subscribe(wb.handleQueueSubscription)
	defer wb.worker.setQueue(dq)

	return NewDistributedPriorityQueue[T](dq)
}

type resultWorkerBinder[T, R any] struct {
	*worker[T, iResultJob[T, R]]
}

func newResultQueues[T, R any](worker *worker[T, iResultJob[T, R]]) IResultWorkerBinder[T, R] {
	return &resultWorkerBinder[T, R]{
		worker: worker,
	}
}

func (rwb *resultWorkerBinder[T, R]) BindQueue() ResultQueue[T, R] {
	return rwb.WithQueue(collections.NewQueue[iResultJob[T, R]]())
}

func (rwb *resultWorkerBinder[T, R]) WithQueue(q IQueue) ResultQueue[T, R] {
	rwb.worker.start()
	return newResultQueue[T, R](rwb.worker, q)
}

func (rwb *resultWorkerBinder[T, R]) BindPriorityQueue() ResultPriorityQueue[T, R] {
	return rwb.WithPriorityQueue(collections.NewPriorityQueue[iResultJob[T, R]]())
}

func (rwb *resultWorkerBinder[T, R]) WithPriorityQueue(pq IPriorityQueue) ResultPriorityQueue[T, R] {
	rwb.worker.start()
	return newResultPriorityQueue[T, R](rwb.worker, pq)
}
