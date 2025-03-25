package gocq

// NewWorker creates a worker that can be bound to different queue types.
// It accepts a worker function that processes items of type T and returns results of type R.
//
// Parameters:
//   - wf: Worker function that processes queue items
//   - config: Optional configuration parameters (concurrency, cache settings, etc.)
//
// If concurrency in config is less than 1 or not provided, it defaults to number of CPU cores.
//
// Example:
//
//	worker := NewWorker(func(data string) (int, error) {
//	    return len(data), nil
//	}, 4) // 4 concurrent workers
//	queue := worker.BindQueue() // Bind to standard queue
func NewWorker[T, R any](wf WorkerFunc[T, R], config ...any) IWorkerBinder[T, R] {
	return newQueues[T, R](newWorker[T, R](wf, config...))
}

// NewErrWorker creates a worker for operations that only return errors (no result value).
// This is useful for fire-and-forget operations where you only care about success/failure.
//
// Parameters:
//   - wf: Worker function that processes items and returns only error
//   - config: Optional configuration parameters (concurrency, cache settings, etc.)
//
// If concurrency in config is less than 1 or not provided, it defaults to number of CPU cores.
//
// Example:
//
//	worker := NewErrWorker(func(data int) error {
//	    log.Printf("Processing: %d", data)
//	    return nil
//	})
//	queue := worker.BindQueue()
func NewErrWorker[T any](wf WorkerErrFunc[T], config ...any) IWorkerBinder[T, any] {
	return newQueues[T, any](newWorker[T, any](wf, config...))
}

// NewVoidWorker creates a worker for operations that don't return any value (void functions).
// This is the most performant worker type as it doesn't allocate result channels.
//
// Parameters:
//   - wf: Worker function that processes items without returning anything
//   - config: Optional configuration parameters (concurrency, cache settings, etc.)
//
// If concurrency in config is less than 1 or not provided, it defaults to number of CPU cores.
//
// Example:
//
//	worker := NewVoidWorker(func(data int) {
//	    fmt.Printf("Processing: %d\n", data)
//	})
//	queue := worker.BindQueue()
func NewVoidWorker[T any](wf VoidWorkerFunc[T], config ...any) IWorkerBinder[T, any] {
	return newQueues[T, any](newWorker[T, any](wf, config...))
}
