package job

import "github.com/fahimfaisaal/gocq/v2/types"

// ResultChannel contains channels for receiving both successful results and errors
// from asynchronous operations. It's designed to provide proper error handling
// for concurrent job processing.
type ResultChannel[R any] chan types.Result[R]

// NewResultChannel creates a new ResultChannel with the specified buffer size.
func NewResultChannel[R any](bufferSize uint32) ResultChannel[R] {
	if bufferSize < 1 {
		panic("buffer size must be greater than 0")
	}

	return make(chan types.Result[R], bufferSize)
}

func (c ResultChannel[R]) Close() error {
	close(c)
	return nil
}
