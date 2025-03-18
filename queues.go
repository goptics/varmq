package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/internal/common"
	"github.com/fahimfaisaal/gocq/v2/types"
)

type ICQueue[R any] interface {
	// IsPaused returns whether the queue is paused.
	IsPaused() bool
	// Restart restarts the queue and initializes the worker goroutines based on the concurrency.
	// Time complexity: O(n) where n is the concurrency
	Restart()
	// Resume continues processing jobs those are pending in the queue.
	Resume() error
	// PendingCount returns the number of Jobs pending in the queue.
	// Time complexity: O(1)
	PendingCount() int
	// CurrentProcessingCount returns the number of Jobs currently being processed.
	// Time complexity: O(1)
	CurrentProcessingCount() uint32
	// WaitUntilFinished waits until all pending Jobs in the queue are processed.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitUntilFinished()
	// Purge removes all pending Jobs from the queue.
	// Time complexity: O(n) where n is the number of pending Jobs
	Purge()
	// WaitAndClose waits until all pending Jobs in the queue are processed and then closes the queue.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitAndClose() error
	// Close closes the queue and resets all internal states.
	// Time complexity: O(n) where n is the number of channels
	Close() error

	GetJob(id string) (types.EnqueuedJob[R], error)
	GetGroupJob(id string) (types.EnqueuedSingleGroupJob[R], error)
}

func withSafeConcurrency(concurrency uint32) uint32 {
	// If concurrency is less than 1, use the number of CPUs as the concurrency
	if concurrency < 1 {
		return common.Cpus()
	}
	return concurrency
}

// Creates a new ConcurrentQueue with the specified concurrency and worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewQueue[T, R any](concurrency uint32, worker Worker[T, R]) ConcurrentQueue[T, R] {
	return newQueue[T, R](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and void worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewVoidQueue[T any](concurrency uint32, worker VoidWorker[T]) ConcurrentQueue[T, any] {
	return newQueue[T, any](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentPriorityQueue with the specified concurrency and worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewPriorityQueue[T, R any](concurrency uint32, worker Worker[T, R]) ConcurrentPriorityQueue[T, R] {
	return newPriorityQueue[T, R](withSafeConcurrency(concurrency), worker)
}

// Creates a new ConcurrentVoidPriorityQueue with the specified concurrency and void worker function.
// if concurrency is less than 1, it will use the number of CPUs as the concurrency
func NewVoidPriorityQueue[T any](concurrency uint32, worker VoidWorker[T]) ConcurrentPriorityQueue[T, any] {
	return newPriorityQueue[T, any](withSafeConcurrency(concurrency), worker)
}
