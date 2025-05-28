package varmq

import "github.com/goptics/varmq/utils"

// NewWorker creates a worker that can be bound to standard, priority, and persistent and distributed queue types.
// If concurrency in config is not provided, it defaults to 1.
// and if concurrency is less than 1, it defaults to number of CPU cores.
// Parameters:
//   - wf: Worker function that processes queue items
//   - config: Optional configuration parameters (concurrency, idle worker ratio, etc.)
//
// Example:
//
//	worker := NewWorker(func(data string) {
//	    return len(data), nil
//	}, 4) // 4 concurrent workers
//	queue := worker.BindQueue() // Bind to standard queue
//	priorityQueue := worker.BindPriorityQueue() // Bind to priority queue
//	persistentQueue := worker.BindPersistentQueue(myPersistentQueue) // Bind to persistent queue
//	distQueue := worker.BindDistributedQueue(myDistributedQueue) // Bind to distributed queue
func NewWorker[T any](wf func(j Job[T]), config ...any) IWorkerBinder[T] {
	return newQueues(newWorker(func(ij iJob[T]) {
		// TODO: The panic error will be passed through inside logger in future
		utils.WithSafe("worker", func() {
			wf(ij)
		})
	}, config...))
}

// NewErrWorker creates a worker for operations that only return errors (no result value).
// This is useful for operations where you only care about success/failure status.
// If concurrency in config is not provided, it defaults to 1.
// and if concurrency is less than 1, it defaults to number of CPU cores.
// Unlike NewWorker, it can't be bound to distributed or persistent queue.
// Parameters:
//   - wf: Worker function that processes items and returns only error
//   - config: Optional configuration parameters (concurrency, idle worker ratio, etc.)
//
// Example:
//
//	worker := NewErrWorker(func(data int) error {
//	    log.Printf("Processing: %d", data)
//	    return nil
//	}, 4) // 4 concurrent workers
//	queue := worker.BindQueue() // Bind to standard queue
//	priorityQueue := worker.BindPriorityQueue() // Bind to priority queue
func NewErrWorker[T any](wf func(j Job[T]) error, config ...any) IErrWorkerBinder[T] {
	return newErrQueues(newErrWorker(func(ij iErrorJob[T]) {
		var panicErr error
		var err error

		panicErr = utils.WithSafe("worker", func() {
			err = wf(ij)
		})

		// send error if any
		if err := utils.SelectError(panicErr, err); err != nil {
			ij.sendError(err)
		}

	}, config...))
}

// NewResultWorker creates a worker for operations that return a result value and error.
// If concurrency in config is not provided, it defaults to 1.
// and if concurrency is less than 1, it defaults to number of CPU cores.
// Unlike NewWorker, it can't be bound to distributed or persistent queue.
// Parameters:
//   - wf: Worker function that processes items and returns a result and error
//   - config: Optional configuration parameters (concurrency, idle worker ratio, etc.)
//
// Example:
//
//	worker := NewResultWorker(func(data string) (int, error) {
//	    return len(data), nil
//	}, 4) // 4 concurrent workers
//	queue := worker.BindQueue() // Bind to standard queue
//	priorityQueue := worker.BindPriorityQueue() // Bind to priority queue
func NewResultWorker[T, R any](wf func(j Job[T]) (R, error), config ...any) IResultWorkerBinder[T, R] {
	return newResultQueues(newResultWorker(func(ij iResultJob[T, R]) {
		var panicErr error
		var err error

		panicErr = utils.WithSafe("worker", func() {
			result, e := wf(ij)
			if e != nil {
				err = e
			} else {
				ij.sendResult(result)
			}
		})

		// send error if any
		if err := utils.SelectError(panicErr, err); err != nil {
			ij.sendError(err)
		}
	}, config...))
}
