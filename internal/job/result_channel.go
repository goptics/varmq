package job

type ResultChannel[R any] struct {
	Data chan R
	Err  chan error
}

// Close closes the data and error channels of a job.
func (c *ResultChannel[R]) Close() error {
	if c.Data != nil {
		close(c.Data)
	}

	if c.Err != nil {
		close(c.Err)
	}

	return nil
}
