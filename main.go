package gocq

// Creates a new ConcurrentQueue with the specified concurrency and worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewWorker[T, R any](wf WorkerFunc[T, R], config ...any) IWorkerBinder[T, R] {
	worker := newWorker[T, R](wf, config...)
	return newQueues[T, R](worker)
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and void worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewEWorker[T any](wf WorkerErrFunc[T], config ...any) IWorkerBinder[T, any] {
	worker := newWorker[T, any](wf, config...)
	return newQueues[T, any](worker)
}

// Creates a new ConcurrentPriorityQueue with the specified concurrency and worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewVoidWorker[T any](wf VoidWorkerFunc[T], config ...any) IWorkerBinder[T, any] {
	worker := newWorker[T, any](wf, config...)
	return newQueues[T, any](worker)
}
