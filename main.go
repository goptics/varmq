package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/shared/utils"
)

func withSafeConcurrency(concurrency uint32) uint32 {
	// If concurrency is less than 1, use the number of CPUs as the concurrency
	if concurrency < 1 {
		return utils.Cpus()
	}
	return concurrency
}

// Creates a new ConcurrentQueue with the specified concurrency and worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewQueue[T, R any](concurrency uint32, worker WorkerFunc[T, R]) ConcurrentQueue[T, R] {
	return newQueue[T, R](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and void worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewVoidQueue[T any](concurrency uint32, worker WorkerErrFunc[T]) ConcurrentQueue[T, any] {
	return newQueue[T, any](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentPriorityQueue with the specified concurrency and worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewPriorityQueue[T, R any](concurrency uint32, worker WorkerFunc[T, R]) ConcurrentPriorityQueue[T, R] {
	return newPriorityQueue[T, R](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentVoidPriorityQueue with the specified concurrency and void worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewVoidPriorityQueue[T any](concurrency uint32, worker WorkerErrFunc[T]) ConcurrentPriorityQueue[T, any] {
	return newPriorityQueue[T, any](withSafeConcurrency(concurrency), worker)
}
