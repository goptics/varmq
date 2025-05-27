package helpers

import (
	"errors"
	"sync/atomic"
)

type Response[R any] struct {
	ch       chan R
	consumed atomic.Bool
	res      R
}

func NewResponse[R any](cap int) *Response[R] {
	return &Response[R]{
		ch:       make(chan R, cap),
		consumed: atomic.Bool{},
	}
}

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

func (rc *Response[R]) Close() error {
	close(rc.ch)
	return nil
}
