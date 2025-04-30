package varmq

import (
	"errors"
	"sync/atomic"
)

// resultChannel contains channels for receiving both successful results and errors
// from asynchronous operations. It's designed to provide proper error handling
// for concurrent job processing.
type resultChannel[R any] struct {
	ch       chan Result[R]
	consumed atomic.Bool
}

// newResultChannel creates a new resultChannel with the specified buffer size.
func newResultChannel[R any](cap uint32) *resultChannel[R] {
	return &resultChannel[R]{
		ch: make(chan Result[R], cap),
	}
}

func (rc *resultChannel[R]) Read() (<-chan Result[R], error) {
	if rc.consumed.CompareAndSwap(false, true) {
		return rc.ch, nil
	}

	return nil, errors.New("result channel has already been consumed")
}

func (c *resultChannel[R]) Send(result Result[R]) {
	c.ch <- result
}

// Close closes the resultChannel.
func (rc *resultChannel[R]) Close() error {
	close(rc.ch)
	return nil
}
