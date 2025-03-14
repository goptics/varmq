package job

import (
	"errors"
	"sync"
)

// Status represents the current state of a job
type Status uint8

const (
	// Queued indicates the job is waiting in the queue to be processed
	Queued Status = iota
	// Processing indicates the job is currently being executed
	Processing
	// Finished indicates the job has completed execution
	Finished
	// Closed indicates the job has been closed and resources freed
	Closed
)

// Job represents a task to be executed by a worker. It maintains the task's
// current status, input data, and channels for receiving results.
type Job[T, R any] struct {
	Status Status
	Data   T
	*ResultChannel[R]
	Lock bool
	mx   sync.Mutex
}

// State returns the current status of the job as a string.
func (j *Job[T, R]) State() string {
	j.mx.Lock()
	defer j.mx.Unlock()

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

// IsClosed returns true if the job has been closed.
func (j *Job[T, R]) IsClosed() bool {
	j.mx.Lock()
	defer j.mx.Unlock()
	return j.Status == Closed
}

// ChangeStatus updates the job's status to the provided value.
func (j *Job[T, R]) ChangeStatus(status Status) {
	j.mx.Lock()
	defer j.mx.Unlock()
	j.Status = status
}

// WaitForResult blocks until the job completes and returns the result and any error.
// If the job's result channel is closed without a value, it returns the zero value
// and any error from the error channel.
func (j *Job[T, R]) WaitForResult() (R, error) {
	data, ok := <-j.ResultChannel.Data

	if ok {
		return data, nil
	}

	return *new(R), <-j.ResultChannel.Err
}

// WaitForError blocks until an error is received on the error channel.
func (j *Job[T, R]) WaitForError() error {
	return <-j.ResultChannel.Err
}

// Drain discards the job's result and error values asynchronously.
// This is useful when you no longer need the results but want to ensure
// the channels are emptied.
func (j *Job[T, R]) Drain() {
	go func() {
		<-j.ResultChannel.Data
		<-j.ResultChannel.Err
	}()
}

// Close closes the job and its associated channels.
// the job regardless of its current state, except when locked.
func (j *Job[T, R]) Close() error {
	j.mx.Lock()
	defer j.mx.Unlock()

	if j.Lock {
		return errors.New("job is not closeable due to lock")
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
