package helpers

type Response[R any] struct {
	ch  chan R
	res R
}

func NewResponse[R any](cap int) *Response[R] {
	return &Response[R]{
		ch: make(chan R, cap),
	}
}

func (rc *Response[R]) Read() <-chan R {
	return rc.ch
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

func (c *Response[R]) Drain() {
	ch := c.Read()

	go func() {
		for range ch {
			// drain all values
		}
	}()
}

func (rc *Response[R]) Close() error {
	close(rc.ch)
	return nil
}
