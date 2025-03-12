package job

type ResultChannel[R any] struct {
	Data chan R
	Err  chan error
}

func (c *ResultChannel[R]) Close() error {
	if c.Data != nil {
		close(c.Data)
	}

	if c.Err != nil {
		close(c.Err)
	}

	return nil
}
