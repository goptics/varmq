package helpers

import (
	"errors"
	"sync/atomic"
)

// Response manages result channels and provides safe operations for receiving
// both successful results and errors from asynchronous operations.
type Response[R any] struct {
	ch       chan R      // Channel for sending/receiving results
	consumed atomic.Bool // Tracks if the channel has been consumed
	res      R           // Stores the last result/error
}

// NewResponse creates a new Response with the specified buffer size.
func NewResponse[R any](cap int) *Response[R] {
	return &Response[R]{
		ch:       make(chan R, cap),
		consumed: atomic.Bool{},
	}
}

// Read returns the underlying channel for reading.
// The channel can only be consumed once.
func (rc *Response[R]) Read() (<-chan R, error) {
	if rc.consumed.CompareAndSwap(false, true) {
		return rc.ch, nil
	}

	return nil, errors.New("result channel has already been consumed")
}

func (c *Response[R]) Send(res R) {
	// Store the result in the result field for later access
	c.res = res
	// Send to channel for immediate consumption
	c.ch <- res
}

// Result blocks until the job completes and returns the result and any error.
// If the job's result channel is closed without a value, it returns the zero value
// and any error from the error channel.
func (c *Response[R]) Response() (R, error) {
	result, ok := <-c.ch

	if ok {
		return result, nil
	}

	return c.res, nil
}

func (c *Response[R]) Drain() error {
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

// Close closes the Response.
func (rc *Response[R]) Close() error {
	close(rc.ch)
	return nil
}
