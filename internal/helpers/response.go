package helpers

type Response[R any] struct {
	ch chan R
}

func NewResponse[R any](cap int) *Response[R] {
	return &Response[R]{
		ch: make(chan R, cap),
	}
}

func (rc *Response[R]) Read() <-chan R {
	return rc.ch
}

// Send is called by every worker goroutine of a group, so it must touch
// nothing but the channel.
func (c *Response[R]) Send(res R) {
	c.ch <- res
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
