package varmq

import "github.com/goptics/varmq/internal/queues"

type workerBinder[T any] struct {
	*worker[T, iJob[T]]
}

// IWorkerBinder is the base interface for binding standard workers to different queue types.
// It provides methods to connect workers with various queue implementations, enabling
// flexible worker-queue configurations. This interface extends beyond basic queue operations
// by supporting both persistence and distribution capabilities, allowing jobs to be
// durably stored and distributed across multiple instances.
// With queue binding capabilities for handling tasks of type T without returning results.
type IWorkerBinder[T any] interface {
	Worker
	// BindQueue binds the worker to a standard Queue implementation.
	// It creates a new Queue with default settings and connects the worker to it.
	// This is the simplest way to get a standard FIFO queue working with this worker.
	//
	// Parameters:
	//   - configs ...QueueConfigFunc: Optional configuration functions to customize the queue behavior.
	//
	// Returns:
	//   - Queue[T]: A fully configured Queue that automatically processes jobs using this worker.
	//
	// Example usage:
	//   queue := worker.BindQueue()
	//   queue.Add(data) // Enqueues a job that will be processed by the worker
	BindQueue(configs ...QueueConfigFunc) Queue[T]

	// WithQueue binds the worker to a custom Queue implementation.
	// This method allows you to use your own queue implementation as long as it
	// satisfies the IQueue interface. This is useful when you need specialized
	// queueing behavior beyond what the standard queue provides.
	//
	// Parameters:
	//   - q IQueue: A custom queue implementation that satisfies the IQueue interface.
	//   - configs ...QueueConfigFunc: Optional configuration functions to customize the queue behavior.
	//
	// Returns:
	//   - Queue[T]: A Queue that uses the provided implementation and processes jobs with this worker.
	//
	// Example usage:
	//   customQueue := NewCustomQueue() // satisfies IQueue interface
	//   queue := worker.WithQueue(customQueue)
	//
	// Shared storage (redisq, sqliteq: IAcknowledgeable) is detected: jobs go
	// out as JSON, an ISubscribable one wakes the worker on enqueue, and Add's
	// job learns its outcome from whichever process runs it.
	WithQueue(q IQueue, configs ...QueueConfigFunc) Queue[T]

	// BindPriorityQueue binds the worker to a standard PriorityQueue implementation.
	// It creates a new PriorityQueue with default settings and connects the worker to it.
	// Use this when you need to process jobs based on priority rather than FIFO order.
	//
	// Parameters:
	//   - configs ...QueueConfigFunc: Optional configuration functions to customize the queue behavior.
	//
	// Returns:
	//   - PriorityQueue[T]: A fully configured PriorityQueue that processes jobs using this worker.
	//
	// Example usage:
	//   priorityQueue := worker.BindPriorityQueue() // satisfies IPriorityQueue interface
	//   priorityQueue.Add(data, 5) // Enqueue with priority 5
	BindPriorityQueue(configs ...QueueConfigFunc) PriorityQueue[T]

	// WithPriorityQueue binds the worker to a custom PriorityQueue implementation.
	// This method allows you to use your own priority queue implementation as long as it
	// satisfies the IPriorityQueue interface. This is useful when you need specialized
	// priority-based queueing beyond what the standard implementation provides.
	//
	// Parameters:
	//   - pq IPriorityQueue: A custom priority queue implementation that satisfies the IPriorityQueue interface.
	//   - configs ...QueueConfigFunc: Optional configuration functions to customize the queue behavior.
	//
	// Returns:
	//   - PriorityQueue[T]: A PriorityQueue that uses the provided implementation and processes jobs with this worker.
	//
	// Example usage:
	//   customPriorityQueue := NewCustomPriorityQueue()
	//   priorityQueue := worker.WithPriorityQueue(customPriorityQueue)
	WithPriorityQueue(pq IPriorityQueue, configs ...QueueConfigFunc) PriorityQueue[T]

	// WithTopic subscribes the worker to a Topic. The subscription reads the
	// topic's shared log through its own cursor and is registered with the
	// worker as a job source, like a bound queue. Every payload published
	// after this call becomes a job for this worker, processed under its
	// normal strategy and concurrency.
	//
	// A worker subscribes to a topic at most once: subscribing it again while
	// the first subscription is attached panics, since it would process every
	// payload twice.
	//
	// Parameters:
	//   - t Topic[T]: The topic to subscribe to, created with NewTopic.
	//   - configs: Optional, for example WithTopicPriority.
	//
	// Returns:
	//   - Subscription[T]: The subscription handle (Len, Purge, Unsubscribe,
	//     Worker). It has no Add; use Topic.Publish.
	//
	// Example usage:
	//   topic := varmq.NewTopic[Order]()
	//   sub := worker.WithTopic(topic)
	//   topic.Publish(order) // the worker receives a copy
	//   sub.Unsubscribe()    // stop receiving
	WithTopic(t Topic[T], configs ...TopicConfigFunc) Subscription[T]
}

func newQueues[T any](worker *worker[T, iJob[T]]) IWorkerBinder[T] {
	// We can return the same workerBinder type but with the IVoidWorkerBinder interface
	// This works because workerBinder implements all methods of IVoidWorkerBinder
	return &workerBinder[T]{worker}
}

// BindQueue creates and binds a new standard queue to the worker
// It returns a Queue interface that can be used to add jobs to the queue
func (wb *workerBinder[T]) BindQueue(configs ...QueueConfigFunc) Queue[T] {
	return wb.WithQueue(queues.NewQueue[iJob[T]](), configs...)
}

// WithQueue binds an existing queue implementation to the worker
// It returns a Queue interface to interact with the queue
func (wb *workerBinder[T]) WithQueue(q IQueue, configs ...QueueConfigFunc) Queue[T] {
	if _, ok := q.(IAcknowledgeable); ok {
		return &durableQueue[T, struct{}, EnqueuedJob, EnqueuedGroupJob]{bindDurable[T, struct{}](wb.worker, q, configs)}
	}
	return newQueue(wb.worker, q, configs...)
}

func (wb *workerBinder[T]) BindPriorityQueue(configs ...QueueConfigFunc) PriorityQueue[T] {
	return wb.WithPriorityQueue(queues.NewPriorityQueue[iJob[T]](), configs...)
}

func (wb *workerBinder[T]) WithPriorityQueue(pq IPriorityQueue, configs ...QueueConfigFunc) PriorityQueue[T] {
	if _, ok := pq.(IAcknowledgeable); ok {
		return &durablePriorityQueue[T, struct{}, EnqueuedJob, EnqueuedGroupJob]{bindDurable[T, struct{}](wb.worker, pq, configs)}
	}
	return newPriorityQueue(wb.worker, pq, configs...)
}

// subscribeQueue forwards q's notifications to w. For a batch source,
// an "enqueued" also pokes its ready hint, and wake-ups replace the persistent
// resync. One wake after Subscribe catches an enqueue that landed between
// the bind's first (empty) pop and Subscribe.
func subscribeQueue[T any, JobType iJob[T]](w *worker[T, JobType], q ISubscribable, src source[JobType]) {
	bs, ok := src.(*batchAckSource[T, JobType])
	if !ok {
		q.Subscribe(func(action string) {
			if action == "enqueued" {
				w.notifyToPullNextJobs()
			}
		})
		w.notifyToPullNextJobs()
		return
	}

	bs.subscribed.Store(true)
	q.Subscribe(func(action string) {
		if action == "enqueued" {
			bs.poke()
			w.notifyToPullNextJobs()
		}
	})
	bs.poke()
	w.notifyToPullNextJobs()
}

func (wb *workerBinder[T]) WithTopic(t Topic[T], configs ...TopicConfigFunc) Subscription[T] {
	if t == nil {
		panic("varmq: WithTopic called with nil Topic")
	}

	return t.subscribe(wb.worker, loadTopicConfigs(configs...).priority)
}

type errWorkerBinder[T any] struct {
	*worker[T, iErrorJob[T]]
}

// IErrWorkerBinder is the interface for binding error workers to different queue types.
// It provides methods to connect error workers with various queue implementations, enabling
// with queue binding capabilities for handling tasks of type T and collecting errors during processing.
type IErrWorkerBinder[T any] interface {
	Worker

	// BindQueue binds the worker to a standard Queue implementation.
	// It creates a new Queue with default settings and connects with the worker.
	//
	// Parameters:
	//   - configs ...QueueConfigFunc: Optional configuration functions to customize the queue behavior.
	//
	// Returns:
	//   - ErrQueue[T]: A fully configured ErrQueue that automatically processes jobs using this worker.
	//
	// Example usage:
	//   errQueue := errWorker.BindQueue()
	//   errJob, ok := errQueue.Add(data)  // Enqueues a job that will be processed by the error worker
	BindQueue(configs ...QueueConfigFunc) ErrQueue[T]
	// WithQueue binds the worker to a custom Queue implementation.
	// This method allows you to use your own queue implementation as long as it
	// satisfies the IQueue interface. This is useful when you need specialized
	// queueing behavior beyond what the standard queue provides.
	//
	// Parameters:
	//   - q IQueue: A custom queue implementation that satisfies the IQueue interface.
	//   - configs ...QueueConfigFunc: Optional configuration functions to customize the queue behavior.
	//
	// Returns:
	//   - ErrQueue[T]: A fully configured ErrQueue that uses the provided implementation and processes jobs with this worker.
	//
	// Example usage:
	//   customQueue := NewCustomQueue()
	//   errQueue := errWorker.WithQueue(customQueue)
	//   errJob, ok := errQueue.Add(data)  // Enqueues a job that will be processed by the error worker
	WithQueue(q IQueue, configs ...QueueConfigFunc) ErrQueue[T]

	// BindPriorityQueue binds the worker to a standard PriorityQueue implementation.
	// It creates a new PriorityQueue with default settings and connects with the worker.
	// Use this when you need to process jobs based on priority rather than FIFO order.
	//
	// Returns:
	//   - ErrPriorityQueue[T]: A fully configured ErrPriorityQueue that automatically processes jobs using this worker.
	//
	// Example usage:
	//   errPriorityQueue := errWorker.BindPriorityQueue()
	//   errJob, ok := errPriorityQueue.Add(data, 5)  // Enqueues a job with priority 5 that will be processed by the error worker
	BindPriorityQueue(configs ...QueueConfigFunc) ErrPriorityQueue[T]

	// WithPriorityQueue binds the worker to a custom PriorityQueue implementation.
	// This method allows you to use your own priority queue implementation as long as it
	// satisfies the IPriorityQueue interface. This is useful when you need specialized
	// priority-based queueing beyond what the standard implementation provides.
	//
	// Parameters:
	//   - pq IPriorityQueue: A custom priority queue implementation that satisfies the IPriorityQueue interface.
	//   - configs ...QueueConfigFunc: Optional configuration functions to customize the queue behavior.
	//
	// Returns:
	//   - ErrPriorityQueue[T]: A fully configured ErrPriorityQueue that uses the provided implementation and processes jobs with this worker.
	//
	// Example usage:
	//   customPriorityQueue := NewCustomPriorityQueue()
	//   errPriorityQueue := errWorker.WithPriorityQueue(customPriorityQueue)
	//   errJob, ok := errPriorityQueue.Add(data, 5)  // Enqueues a job with priority 5 that will be processed by the error worker
	WithPriorityQueue(pq IPriorityQueue, configs ...QueueConfigFunc) ErrPriorityQueue[T]
}

func newErrQueues[T any](worker *worker[T, iErrorJob[T]]) IErrWorkerBinder[T] {
	return &errWorkerBinder[T]{worker}
}

func (ewb *errWorkerBinder[T]) BindQueue(configs ...QueueConfigFunc) ErrQueue[T] {
	return ewb.WithQueue(queues.NewQueue[iErrorJob[T]](), configs...)
}

func (ewb *errWorkerBinder[T]) WithQueue(q IQueue, configs ...QueueConfigFunc) ErrQueue[T] {
	if _, ok := q.(IAcknowledgeable); ok {
		return &durableQueue[T, struct{}, EnqueuedErrJob, EnqueuedErrGroupJob]{bindDurable[T, struct{}](ewb.worker, q, configs)}
	}
	return newErrorQueue(ewb.worker, q, configs...)
}

func (ewb *errWorkerBinder[T]) BindPriorityQueue(configs ...QueueConfigFunc) ErrPriorityQueue[T] {
	return ewb.WithPriorityQueue(queues.NewPriorityQueue[iErrorJob[T]](), configs...)
}

func (ewb *errWorkerBinder[T]) WithPriorityQueue(pq IPriorityQueue, configs ...QueueConfigFunc) ErrPriorityQueue[T] {
	if _, ok := pq.(IAcknowledgeable); ok {
		return &durablePriorityQueue[T, struct{}, EnqueuedErrJob, EnqueuedErrGroupJob]{bindDurable[T, struct{}](ewb.worker, pq, configs)}
	}
	return newErrorPriorityQueue(ewb.worker, pq, configs...)
}

type resultWorkerBinder[T, R any] struct {
	*worker[T, iResultJob[T, R]]
}

// IResultWorkerBinder is the base interface for binding workers to different queue types.
// It provides methods to connect workers with various queue implementations, enabling
// flexible worker-queue configurations.
// With queue binding capabilities for handling tasks of type T and producing results of type R.
type IResultWorkerBinder[T, R any] interface {
	Worker

	// BindQueue binds the worker to a standard Queue implementation.
	// It creates a new Queue with default settings and connects the worker to it.
	// This is the simplest way to get a standard FIFO queue working with this worker.
	//
	// Parameters:
	//   - configs ...QueueConfigFunc: Optional configuration functions to customize the queue behavior.
	//
	// Returns:
	//   - ResultQueue[T, R]: A fully configured Result Queue that automatically processes jobs using this worker.
	//
	// Example usage:
	//   queue := worker.BindQueue()
	//   queue.Add(data) // Enqueues a job that will be processed by the worker
	BindQueue(configs ...QueueConfigFunc) ResultQueue[T, R]

	// WithQueue binds the worker to a custom Queue implementation.
	// This method allows you to use your own queue implementation as long as it
	// satisfies the IQueue interface. This is useful when you need specialized
	// queueing behavior beyond what the standard queue provides.
	//
	// Parameters:
	//   - q IQueue: A custom queue implementation that satisfies the IQueue interface.
	//   - configs ...QueueConfigFunc: Optional configuration functions to customize the queue behavior.
	//
	// Returns:
	//   - ResultQueue[T, R]: A Result Queue that uses the provided implementation and processes jobs with this worker.
	//
	// Example usage:
	//   customQueue := NewCustomQueue() // satisfies IQueue interface
	//   queue := worker.WithQueue(customQueue)
	WithQueue(q IQueue, configs ...QueueConfigFunc) ResultQueue[T, R]

	// BindPriorityQueue binds the worker to a standard PriorityQueue implementation.
	// It creates a new PriorityQueue with default settings and connects the worker to it.
	// Use this when you need to process jobs based on priority rather than FIFO order.
	//
	// Parameters:
	//   - configs ...QueueConfigFunc: Optional configuration functions to customize the queue behavior.
	//
	// Returns:
	//   - ResultPriorityQueue[T, R]: A fully configured ResultPriorityQueue that processes jobs using this worker.
	//
	// Example usage:
	//   priorityQueue := worker.BindPriorityQueue() // satisfies IPriorityQueue interface
	//   priorityQueue.Add(data, 5) // Enqueue with priority 5
	BindPriorityQueue(configs ...QueueConfigFunc) ResultPriorityQueue[T, R]

	// WithPriorityQueue binds the worker to a custom PriorityQueue implementation.
	// This method allows you to use your own priority queue implementation as long as it
	// satisfies the IPriorityQueue interface. This is useful when you need specialized
	// priority-based queueing beyond what the standard implementation provides.
	//
	// Parameters:
	//   - pq IPriorityQueue: A custom priority queue implementation that satisfies the IPriorityQueue interface.
	//   - configs ...QueueConfigFunc: Optional configuration functions to customize the queue behavior.
	//
	// Returns:
	//   - ResultPriorityQueue[T, R]: A Result PriorityQueue that uses the provided implementation and processes jobs with this worker.
	//
	// Example usage:
	//   customPriorityQueue := NewCustomPriorityQueue()
	//   priorityQueue := worker.WithPriorityQueue(customPriorityQueue)
	WithPriorityQueue(pq IPriorityQueue, configs ...QueueConfigFunc) ResultPriorityQueue[T, R]
}

func newResultQueues[T, R any](worker *worker[T, iResultJob[T, R]]) IResultWorkerBinder[T, R] {
	return &resultWorkerBinder[T, R]{worker}
}

func (rwb *resultWorkerBinder[T, R]) BindQueue(configs ...QueueConfigFunc) ResultQueue[T, R] {
	return rwb.WithQueue(queues.NewQueue[iResultJob[T, R]](), configs...)
}

func (rwb *resultWorkerBinder[T, R]) WithQueue(q IQueue, configs ...QueueConfigFunc) ResultQueue[T, R] {
	if _, ok := q.(IAcknowledgeable); ok {
		return &durableQueue[T, R, EnqueuedResultJob[R], EnqueuedResultGroupJob[R]]{bindDurable[T, R](rwb.worker, q, configs)}
	}
	return newResultQueue(rwb.worker, q, configs...)
}

func (rwb *resultWorkerBinder[T, R]) BindPriorityQueue(configs ...QueueConfigFunc) ResultPriorityQueue[T, R] {
	return rwb.WithPriorityQueue(queues.NewPriorityQueue[iResultJob[T, R]](), configs...)
}

func (rwb *resultWorkerBinder[T, R]) WithPriorityQueue(pq IPriorityQueue, configs ...QueueConfigFunc) ResultPriorityQueue[T, R] {
	if _, ok := pq.(IAcknowledgeable); ok {
		return &durablePriorityQueue[T, R, EnqueuedResultJob[R], EnqueuedResultGroupJob[R]]{bindDurable[T, R](rwb.worker, pq, configs)}
	}
	return newResultPriorityQueue(rwb.worker, pq, configs...)
}
