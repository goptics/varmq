package varmq

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
func NewWorker[T any](wf WorkerFunc[T], config ...any) IWorkerBinder[T] {
	return newQueues(newWorker(wf, config...))
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
func NewErrWorker[T any](wf WorkerErrFunc[T], config ...any) IErrWorkerBinder[T] {
	return newErrQueues(newErrWorker(wf, config...))
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
func NewResultWorker[T, R any](wf WorkerResultFunc[T, R], config ...any) IResultWorkerBinder[T, R] {
	return newResultQueues(newResultWorker(wf, config...))
}
