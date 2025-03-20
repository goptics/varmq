package job

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/v2/shared/types"
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
	Input         T
	status        atomic.Uint32
	Output        *types.Result[R]
}

// jobView represents a view of a job's state for serialization.
type jobView[T, R any] struct {
	Id     string           `json:"id"`
	Status string           `json:"status"`
	Input  T                `json:"input"`
	Output *types.Result[R] `json:"output"`
}

type Job[T, R any] interface {
	types.IJob
	types.EnqueuedJob[R]
	Data() T
	SaveAndSendResult(result R)
	SaveAndSendError(err error)
	ChangeStatus(status Status) Job[T, R]
	CloseResultChannel()
}

// New creates a new job with the provided data.
func New[T, R any](data T, id ...string) *job[T, R] {
	return &job[T, R]{
		id:            strings.Join(id, "-"),
		Input:         data,
		resultChannel: NewResultChannel[R](1),
		status:        atomic.Uint32{},
		Output:        &types.Result[R]{},
	}
}

func (j *job[T, R]) ID() string {
	return j.id
}

func (j *job[T, R]) Data() T {
	return j.Input
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

// SaveAndSendResult sends a result to the job's result channel.
func (j *job[T, R]) SaveAndSendResult(result R) {
	r := types.Result[R]{Data: result}
	j.Output = &r
	j.resultChannel.Send(r)
}

// SaveAndSendError sends an error to the job's result channel.
func (j *job[T, R]) SaveAndSendError(err error) {
	r := types.Result[R]{Err: err}
	j.Output = &r
	j.resultChannel.Send(r)
}

// Result blocks until the job completes and returns the result and any error.
// If the job's result channel is closed without a value, it returns the zero value
// and any error from the error channel.
func (j *job[T, R]) Result() (R, error) {
	result, ok := <-j.resultChannel.ch

	if ok {
		return result.Data, result.Err
	}

	return j.Output.Data, j.Output.Err
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

func (j *job[T, R]) Json() ([]byte, error) {
	view := jobView[T, R]{
		Id:     j.ID(),
		Status: j.Status(),
		Input:  j.Input,
		Output: j.Output,
	}

	return json.Marshal(view)
}

func ParseToJob[T, R any](data []byte) (Job[T, R], error) {
	var view jobView[T, R]
	if err := json.Unmarshal(data, &view); err != nil {
		return nil, fmt.Errorf("failed to parse job: %w", err)
	}

	j := &job[T, R]{
		id:            view.Id,
		Input:         view.Input,
		Output:        view.Output,
		resultChannel: NewResultChannel[R](1),
	}

	// Set the status
	switch view.Status {
	case "Created":
		j.status.Store(Created)
	case "Queued":
		j.status.Store(Queued)
	case "Processing":
		j.status.Store(Processing)
	case "Finished":
		j.status.Store(Finished)
	case "Closed":
		j.status.Store(Closed)
	default:
		return nil, fmt.Errorf("invalid status: %s", view.Status)
	}

	return j, nil
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
