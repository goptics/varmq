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

type Identifiable interface {
	ID() string
}

type StatusProvider interface {
	IsClosed() bool
	Status() string
}

type Awaitable interface {
	// Wait blocks until the job is closed.
	Wait()
}

type Drainer interface {
	Drain() error
}

// ResultController manages result channels and provides safe operations for receiving
// both successful results and errors from asynchronous operations.
type ResultController[R any] struct {
	ch       chan Result[R] // Channel for sending/receiving results
	consumed atomic.Bool    // Tracks if the channel has been consumed
	result   Result[R]      // Stores the last result/error
}

// newResultController creates a new ResultController with the specified buffer size.
func newResultController[R any](cap int) *ResultController[R] {
	return &ResultController[R]{
		ch:       make(chan Result[R], cap),
		consumed: atomic.Bool{},
		result:   Result[R]{},
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
	// Store the result in the result field for later access
	c.result = result
	// Send to channel for immediate consumption
	c.ch <- result
}

// Result blocks until the job completes and returns the result and any error.
// If the job's result channel is closed without a value, it returns the zero value
// and any error from the error channel.
func (c *ResultController[R]) Result() (R, error) {
	result, ok := <-c.ch

	if ok {
		return result.Data, result.Err
	}

	return c.result.Data, c.result.Err
}

func (c *ResultController[R]) Drain() error {
	ch, err := c.Read()

	if err != nil {
		return err
	}

	go func() {
		for range ch {
			// drain
		}
	}()

	return nil
}

// Close closes the ResultController.
func (rc *ResultController[R]) Close() error {
	close(rc.ch)
	return nil
}
