package job

import (
	"errors"
	"strings"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/v2/types"
)

type Status = uint32

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

// job represents a task to be executed by a worker. It maintains the task's
// current status, input data, and channels for receiving results.
type job[T, R any] struct {
	id            string
	resultChannel *ResultChannel[R]
	data          T
	status        atomic.Uint32
}

type Job[T, R any] interface {
	types.IJob
	types.EnqueuedJob[R]
	Data() T
	SendResult(result R)
	SendError(err error)
	ChangeStatus(status Status) Job[T, R]
	CloseResultChannel()
}

// New creates a new job with the provided data.
func New[T, R any](data T, id ...string) *job[T, R] {
	return &job[T, R]{
		id:            strings.Join(id, "-"),
		data:          data,
		resultChannel: NewResultChannel[R](1),
		status:        atomic.Uint32{},
	}
}

func (j *job[T, R]) ID() string {
	return j.id
}

func (j *job[T, R]) Data() T {
	return j.data
}

// State returns the current status of the job as a string.
func (j *job[T, R]) Status() string {
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
func (j *job[T, R]) IsClosed() bool {
	return j.status.Load() == Closed
}

// ChangeStatus updates the job's status to the provided value.
func (j *job[T, R]) ChangeStatus(status Status) Job[T, R] {
	j.status.Store(status)
	return j
}

// SendResult sends a result to the job's result channel.
func (j *job[T, R]) SendResult(result R) {
	j.resultChannel.Send(types.Result[R]{Data: result})
}

// SendError sends an error to the job's result channel.
func (j *job[T, R]) SendError(err error) {
	j.resultChannel.Send(types.Result[R]{Err: err})
}

// Result blocks until the job completes and returns the result and any error.
// If the job's result channel is closed without a value, it returns the zero value
// and any error from the error channel.
func (j *job[T, R]) Result() (R, error) {
	result, ok := <-j.resultChannel.ch

	if ok {
		return result.Data, result.Err
	}

	return *new(R), errors.New("job is closed")
}

// Drain discards the job's result and error values asynchronously.
// This is useful when you no longer need the results but want to ensure
// the channels are emptied.
func (j *job[T, R]) Drain() error {
	ch := j.resultChannel.Read()

	if ch == nil {
		return errors.New("result channel has already been consumed")
	}

	go func() {
		for range ch {
			// drain
		}
	}()

	return nil
}

func (j *job[T, R]) CloseResultChannel() {
	j.resultChannel.Close()
}

func (j *job[T, R]) isCloseable() error {
	switch j.status.Load() {
	case Processing:
		return errors.New("job is processing, you can't close processing job")
	case Closed:
		return errors.New("job is already closed")
	}

	return nil
}

// Close closes the job and its associated channels.
// the job regardless of its current state, except when locked.
func (j *job[T, R]) Close() error {
	if err := j.isCloseable(); err != nil {
		return err
	}

	j.resultChannel.Close()
	j.status.Store(Closed)

	return nil
}
