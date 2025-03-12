package queue

import (
	"errors"
)

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

// Job represents a task to be executed by a worker.
type Job[T, R any] struct {
	Data T
	*ResultChannel[R]
	Lock bool // if true, channel will not be closed by this job
}

func (j *Job[T, R]) Wait() (R, error) {
	data, ok := <-j.ResultChannel.Data

	if ok {
		return data, nil
	}

	return *new(R), <-j.ResultChannel.Err
}

func (j *Job[T, R]) Close() error {
	if j.Lock {
		return errors.New("job is not closeable")
	}

	j.ResultChannel.Close()
	return nil
}
