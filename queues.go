package gocq

import (
	cq "github.com/fahimfaisaal/gocq/internal/concurrent_queue"
	vq "github.com/fahimfaisaal/gocq/internal/concurrent_queue/void_queue"
	"github.com/fahimfaisaal/gocq/internal/shared"
)

func withSafeConcurrency(concurrency uint32) uint32 {
	// If concurrency is less than 1, use the number of CPUs as the concurrency
	if concurrency < 1 {
		return shared.Cpus()
	}
	return concurrency
}

// Creates a new ConcurrentQueue with the specified concurrency and worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewQueue[T, R any](concurrency uint32, worker cq.Worker[T, R]) ConcurrentQueue[T, R] {
	return cq.NewQueue[T, R](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and void worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewVoidQueue[T any](concurrency uint32, worker cq.VoidWorker[T]) ConcurrentVoidQueue[T] {
	return vq.NewQueue[T](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentPriorityQueue with the specified concurrency and worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewPriorityQueue[T, R any](concurrency uint32, worker cq.Worker[T, R]) ConcurrentPriorityQueue[T, R] {
	return cq.NewPriorityQueue[T, R](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentVoidPriorityQueue with the specified concurrency and void worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewVoidPriorityQueue[T any](concurrency uint32, worker cq.VoidWorker[T]) ConcurrentVoidPriorityQueue[T] {
	return vq.NewPriorityQueue[T](withSafeConcurrency(concurrency), worker)
}
