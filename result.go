package varmq

import (
	"errors"
	"sync/atomic"
)

// Result represents the result of a job, containing the data and any error that occurred.
type Result[T any] struct {
	JobId string
	Data  T
	Err   error
}

type Job interface {
	// ID returns the unique identifier of the job.
	ID() string
	// IsClosed returns whether the job is closed.
	IsClosed() bool
	// Status returns the current status of the job.
	Status() string
	// close closes the job and its associated channels.
	close() error
}

// EnqueuedJob represents a job that has been enqueued and can wait for a result.
type EnqueuedJob[R any] interface {
	Job
	// Drain discards the job's result and error values asynchronously.
	Drain() error
	// Result blocks until the job completes and returns the result and any error.
	Result() (R, error)
}

type EnqueuedGroupJob[T any] interface {
	// Len returns the number of jobs in the group.
	Len() int
	// Wait blocks until all jobs in the group are completed.
	Wait()
	// Drain discards the job's result and error values asynchronously.
	Drain() error
	// Results returns a channel that will receive the results of the group
	Results() (<-chan Result[T], error)
}

type EnqueuedSingleGroupJob[R any] interface {
	Job
	EnqueuedGroupJob[R]
}

// ResultController manages result channels and provides safe operations for receiving
// both successful results and errors from asynchronous operations.
type ResultController[R any] struct {
	ch       chan Result[R] // Channel for sending/receiving results
	consumed atomic.Bool    // Tracks if the channel has been consumed
	Output   Result[R]      // Stores the last result/error
}

// newResultController creates a new ResultController with the specified buffer size.
func newResultController[R any](cap int) *ResultController[R] {
	return &ResultController[R]{
		ch:       make(chan Result[R], cap),
		consumed: atomic.Bool{},
		Output:   Result[R]{},
	}
}

// Read returns the underlying channel for reading.
// The channel can only be consumed once.
func (rc *ResultController[R]) Read() (<-chan Result[R], error) {
	if rc.consumed.CompareAndSwap(false, true) {
		return rc.ch, nil
	}

	return nil, errors.New("result channel has already been consumed")
}

func (c *ResultController[R]) Send(result Result[R]) {
	// Store the result in the Output field for later access
	c.Output = result
	// Send to channel for immediate consumption
	c.ch <- result
}

// Close closes the ResultController.
func (rc *ResultController[R]) Close() error {
	close(rc.ch)
	return nil
}
