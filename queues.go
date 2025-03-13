package gocq

import (
	cq "github.com/fahimfaisaal/gocq/internal/concurrent_queue"
	vq "github.com/fahimfaisaal/gocq/internal/concurrent_queue/void_queue"
)

type ConcurrentQueue[T, R any] interface {
	cq.IConcurrentQueue[T, R]
}

type ConcurrentVoidQueue[T any] interface {
	vq.IConcurrentVoidQueue[T]
}

type ConcurrentPriorityQueue[T, R any] interface {
	cq.IConcurrentPriorityQueue[T, R]
}

type ConcurrentVoidPriorityQueue[T any] interface {
	vq.IConcurrentVoidPriorityQueue[T]
}

func withSafeConcurrency(concurrency uint32) uint32 {
	// Ensure concurrency is at least 1
	if concurrency < 1 {
		return 1
	}
	return concurrency
}

// Creates a new ConcurrentQueue with the specified concurrency and worker function.
func NewQueue[T, R any](concurrency uint32, worker cq.Worker[T, R]) ConcurrentQueue[T, R] {
	return cq.NewQueue[T, R](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and void worker function.
func NewVoidQueue[T any](concurrency uint32, worker cq.VoidWorker[T]) ConcurrentVoidQueue[T] {
	return vq.NewQueue[T](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentPriorityQueue with the specified concurrency and worker function.
func NewPriorityQueue[T, R any](concurrency uint32, worker cq.Worker[T, R]) ConcurrentPriorityQueue[T, R] {
	return cq.NewPriorityQueue[T, R](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentVoidPriorityQueue with the specified concurrency and void worker function.
func NewVoidPriorityQueue[T any](concurrency uint32, worker cq.VoidWorker[T]) ConcurrentVoidPriorityQueue[T] {
	return vq.NewPriorityQueue[T](withSafeConcurrency(concurrency), worker)
}
