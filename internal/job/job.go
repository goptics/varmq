package job

import (
	"errors"
	"sync"
	"sync/atomic"
)

// Status represents the current state of a job
type Status uint8

const (
	// Created indicates the job has been created but not yet queued
	Created Status = iota
	// Queued indicates the job is waiting in the queue to be processed
	Queued
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
	lock atomic.Bool
	mx   sync.Mutex
}

// New creates a new Job with the provided data.
func New[T, R any](data T) *Job[T, R] {
	return &Job[T, R]{
		Status:        Created,
		Data:          data,
		ResultChannel: NewResultChannel[R](1),
	}
}

func NewWithResultChannel[T, R any](resultChannel *ResultChannel[R]) *Job[T, R] {
	return &Job[T, R]{
		Status:        Created,
		Data:          *new(T),
		ResultChannel: resultChannel,
	}
}

// Init returns a initial Job with the provided data. with same result channel and including old
func (j *Job[T, R]) Init(data T) *Job[T, R] {
	newJob := &Job[T, R]{
		Status:        j.Status,
		Data:          data,
		ResultChannel: j.ResultChannel,
	}
	// Transfer the lock state without copying
	if j.IsLocked() {
		newJob.Lock()
	}
	return newJob
}

func (j *Job[T, R]) IsLocked() bool {
	return j.lock.Load()
}

func (j *Job[T, R]) Lock() *Job[T, R] {
	j.lock.Store(true)
	return j
}

func (j *Job[T, R]) Unlock() *Job[T, R] {
	j.lock.Store(false)
	return j
}

// State returns the current status of the job as a string.
func (j *Job[T, R]) State() string {
	j.mx.Lock()
	defer j.mx.Unlock()

	switch j.Status {
	case Created:
		return "Created"
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
func (j *Job[T, R]) ChangeStatus(status Status) *Job[T, R] {
	j.mx.Lock()
	defer j.mx.Unlock()
	j.Status = status

	return j
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

	if j.IsLocked() {
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
