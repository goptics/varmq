package job

import (
	"errors"
	"sync"
)

type Status uint8

const (
	Queued     Status = iota
	Processing Status = iota
	Finished   Status = iota
	Closed     Status = iota
)

// Job represents a task to be executed by a worker.
type Job[T, R any] struct {
	Status Status
	Data   T
	*ResultChannel[R]
	Lock bool
	mx   sync.Mutex
}

func (j *Job[T, R]) State() string {
	defer j.mx.Unlock()
	j.mx.Lock()
	switch j.Status {
	case Queued:
		return "Queued"
	case Processing:
		return "Processing"
	case Finished:
		return "Finished"
	case Closed:
		return "Closed"
	default:
		return "Unknown"
	}
}

func (j *Job[T, R]) IsClosed() bool {
	defer j.mx.Unlock()
	j.mx.Lock()
	return j.Status == Closed
}

func (j *Job[T, R]) ChangeStatus(status Status) {
	defer j.mx.Unlock()
	j.mx.Lock()
	j.Status = status
}

func (j *Job[T, R]) WaitForResult() (R, error) {
	data, ok := <-j.ResultChannel.Data

	if ok {
		return data, nil
	}

	return *new(R), <-j.ResultChannel.Err
}

func (j *Job[T, R]) WaitForError() error {
	return <-j.ResultChannel.Err
}

func (j *Job[T, R]) Drain() {
	go func() {
		<-j.ResultChannel.Data
		<-j.ResultChannel.Err
	}()
}

func (j *Job[T, R]) Close() error {
	defer j.mx.Unlock()
	j.mx.Lock()
	if j.Lock {
		return errors.New("job is not closeable")
	}
	switch j.Status {
	case Processing:
		return errors.New("job is processing")
	case Closed:
		return errors.New("job is already closed")
	}

	j.ResultChannel.Close()
	j.Status = Closed

	return nil
}
