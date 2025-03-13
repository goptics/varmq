package job

// ResultChannel contains channels for receiving both successful results and errors
// from asynchronous operations. It's designed to provide proper error handling
// for concurrent job processing.
type ResultChannel[R any] struct {
	Data chan R
	Err  chan error
}

// Close safely closes both the Data and Err channels if they're not nil.
// It always returns nil, indicating successful closure.
func (c *ResultChannel[R]) Close() error {
	if c.Data != nil {
		close(c.Data)
	}

	if c.Err != nil {
		close(c.Err)
	}

	return nil
}
