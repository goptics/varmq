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

func NewQueue[T, R any](concurrency uint32, worker cq.Worker[T, R]) ConcurrentQueue[T, R] {
	return cq.NewQueue[T, R](concurrency, worker)
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and worker function.
func NewVoidQueue[T any](concurrency uint32, worker cq.VoidWorker[T]) ConcurrentVoidQueue[T] {
	return vq.NewQueue[T](concurrency, worker)
}

func NewPriorityQueue[T, R any](concurrency uint32, worker cq.Worker[T, R]) ConcurrentPriorityQueue[T, R] {
	return cq.NewPriorityQueue[T, R](concurrency, worker)
}

func NewVoidPriorityQueue[T any](concurrency uint32, worker cq.VoidWorker[T]) ConcurrentVoidPriorityQueue[T] {
	return vq.NewPriorityQueue[T](concurrency, worker)
}
