package job

import (
	"errors"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/v2/types"
)

// status represents the current state of a job

const (
	// Created indicates the job has been created but not yet queued
	Created uint32 = iota
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
	resultChannel ResultChannel[R]
	data          T
	status        atomic.Uint32
	lock          atomic.Bool
}

type IJob[T, R any] interface {
	types.EnqueuedJob[R]
	Data() T
	SendResult(result R)
	SendError(err error)
	ChangeStatus(status uint32) IJob[T, R]
	CloseResultChannel()
}

// New creates a new Job with the provided data.
func New[T, R any](data T) *Job[T, R] {
	return &Job[T, R]{
		data:          data,
		resultChannel: NewResultChannel[R](),
		status:        atomic.Uint32{},
	}
}

func (j *Job[T, R]) IsLocked() bool {
	return j.lock.Load()
}

func (j *Job[T, R]) Lock() types.IJob {
	j.lock.Store(true)
	return j
}

func (j *Job[T, R]) Unlock() types.IJob {
	j.lock.Store(false)
	return j
}

func (j *Job[T, R]) Data() T {
	return j.data
}

// State returns the current status of the job as a string.
func (j *Job[T, R]) Status() string {
	switch j.status.Load() {
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
	return j.status.Load() == Closed
}

// ChangeStatus updates the job's status to the provided value.
func (j *Job[T, R]) ChangeStatus(status uint32) IJob[T, R] {
	j.status.Store(status)
	return j
}

func (j *Job[T, R]) SendResult(result R) {
	j.resultChannel <- types.Result[R]{Data: result}
}

func (j *Job[T, R]) SendError(err error) {
	j.resultChannel <- types.Result[R]{Err: err}
}

// WaitForResult blocks until the job completes and returns the result and any error.
// If the job's result channel is closed without a value, it returns the zero value
// and any error from the error channel.
func (j *Job[T, R]) WaitForResult() (R, error) {
	result, ok := <-j.resultChannel

	if ok {
		return result.Data, result.Err
	}

	return *new(R), nil
}

// Drain discards the job's result and error values asynchronously.
// This is useful when you no longer need the results but want to ensure
// the channels are emptied.
func (j *Job[T, R]) Drain() {
	go func() {
		<-j.resultChannel
	}()
}

func (j *Job[T, R]) CloseResultChannel() {
	j.resultChannel.Close()
}

// Close closes the job and its associated channels.
// the job regardless of its current state, except when locked.
func (j *Job[T, R]) Close() error {
	if j.IsLocked() {
		return errors.New("job is not closeable due to lock")
	}

	switch j.status.Load() {
	case Processing:
		return errors.New("job is processing")
	case Closed:
		return errors.New("job is already closed")
	}

	j.resultChannel.Close()
	j.status.Store(Closed)

	return nil
}
