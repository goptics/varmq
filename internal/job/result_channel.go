package job

// ResultChannel contains channels for receiving both successful results and errors
// from asynchronous operations. It's designed to provide proper error handling
// for concurrent job processing.
type ResultChannel[R any] struct {
	Data chan R
	Err  chan error
}

// NewResultChannel creates a new ResultChannel with the specified buffer size.
func NewResultChannel[R any](bufferSize uint32) *ResultChannel[R] {
	if bufferSize < 1 {
		panic("buffer size must be greater than 0")
	}

	return &ResultChannel[R]{
		Data: make(chan R, bufferSize),
		Err:  make(chan error, bufferSize),
	}
}

// NewVoidResultChannel creates a new ResultChannel with nil Data channel.
func NewVoidResultChannel() *ResultChannel[any] {
	return &ResultChannel[any]{
		Err: make(chan error, 1),
	}
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
