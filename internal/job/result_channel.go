package job

import (
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/v2/types"
)

// ResultChannel contains channels for receiving both successful results and errors
// from asynchronous operations. It's designed to provide proper error handling
// for concurrent job processing.
type ResultChannel[R any] struct {
	ch       chan types.Result[R]
	consumed atomic.Bool
}

// NewResultChannel creates a new ResultChannel with the specified buffer size.
func NewResultChannel[R any](cap uint32) *ResultChannel[R] {
	return &ResultChannel[R]{
		ch: make(chan types.Result[R], cap),
	}
}

func (rc *ResultChannel[R]) Read() <-chan types.Result[R] {
	if rc.consumed.CompareAndSwap(false, true) {
		return rc.ch
	}

	return nil
}

func (c *ResultChannel[R]) Send(result types.Result[R]) {
	c.ch <- result
}

// Close closes the ResultChannel.
func (rc *ResultChannel[R]) Close() error {
	close(rc.ch)
	return nil
}
