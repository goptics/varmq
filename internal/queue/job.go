package queue

import (
	"errors"
)

type Channel[R any] struct {
	Data chan R
	Err  chan error
}

func (c *Channel[R]) Close() error {
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
	Channel[R]
	Lock bool
}

func (j *Job[T, R]) Wait() (R, error) {
	data, ok := <-j.Channel.Data

	if ok {
		return data, nil
	}

	return *new(R), <-j.Channel.Err
}

func (j *Job[T, R]) Close() error {
	if j.Lock {
		return errors.New("job channel is not closeable")
	}

	j.Channel.Close()
	return nil
}
