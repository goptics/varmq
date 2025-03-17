package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/internal/common"
	cq "github.com/fahimfaisaal/gocq/v2/internal/concurrent_queue"
	"github.com/fahimfaisaal/gocq/v2/types"
)

func withSafeConcurrency(concurrency uint32) uint32 {
	// If concurrency is less than 1, use the number of CPUs as the concurrency
	if concurrency < 1 {
		return common.Cpus()
	}
	return concurrency
}

// Creates a new ConcurrentQueue with the specified concurrency and worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewQueue[T, R any](concurrency uint32, worker types.Worker[T, R]) types.IConcurrentQueue[T, R] {
	return cq.NewQueue[T, R](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and void worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewVoidQueue[T any](concurrency uint32, worker types.VoidWorker[T]) types.IConcurrentQueue[T, any] {
	return cq.NewQueue[T, any](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentPriorityQueue with the specified concurrency and worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewPriorityQueue[T, R any](concurrency uint32, worker types.Worker[T, R]) types.IConcurrentPriorityQueue[T, R] {
	return cq.NewPriorityQueue[T, R](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentVoidPriorityQueue with the specified concurrency and void worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewVoidPriorityQueue[T any](concurrency uint32, worker types.VoidWorker[T]) types.IConcurrentPriorityQueue[T, any] {
	return cq.NewPriorityQueue[T, any](withSafeConcurrency(concurrency), worker)
}
