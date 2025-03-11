package queue

import (
	"errors"
)

type Channel[R any] struct {
	Data chan R
	Err  chan error
}

// Job represents a task to be executed by a worker.
type Job[T, R any] struct {
	Data T
	Channel[R]
	Lock bool
}

func (j *Job[T, R]) Wait() (R, error) {
	select {
	case data := <-j.Channel.Data:
		return data, nil
	case err := <-j.Channel.Err:
		return *new(R), err
	}
}

func (j *Job[T, R]) Close() error {
	if j.Lock {
		return errors.New("job channel is not closeable")
	}

	if j.Channel.Data != nil {
		close(j.Channel.Data)
	}
	close(j.Channel.Err)
	return nil
}
