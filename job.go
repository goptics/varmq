package gocq

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
)

const (
	// created indicates the job has been created but not yet queued
	created status = iota
	// queued indicates the job is waiting in the queue to be processed
	queued
	// processing indicates the job is currently being executed
	processing
	// finished indicates the job has completed execution
	finished
	// closed indicates the job has been closed and resources freed
	closed
)

// job represents a task to be executed by a worker. It maintains the task's
// current status, input data, and channels for receiving results.
type job[T, R any] struct {
	id            string
	resultChannel *resultChannel[R]
	Input         T
	status        atomic.Uint32
	Output        *Result[R]
	priority      int
}

// jobView represents a view of a job's state for serialization.
type jobView[T, R any] struct {
	Id       string     `json:"id"`
	Status   string     `json:"status"`
	Input    T          `json:"input"`
	Output   *Result[R] `json:"output"`
	Priority int        `json:"priority"`
}

type Job interface {
	// ID returns the unique identifier of the job.
	ID() string
	// IsClosed returns whether the job is closed.
	IsClosed() bool
	// Status returns the current status of the job.
	Status() string
	// Close closes the job and its associated channels.
	Close() error
	// Json returns the JSON representation of the job.
	Json() ([]byte, error)
}

type iJob[T, R any] interface {
	Job
	EnqueuedJob[R]
	// Data returns the input data of the job.
	Data() T
	// SaveAndSendResult saves the result and sends it to the job's result channel.
	SaveAndSendResult(result R)
	// SaveAndSendError saves the error and sends it to the job's error channel.
	SaveAndSendError(err error)
	// ChangeStatus changes the status of the job.
	ChangeStatus(s status) iJob[T, R]
	// CloseResultChannel closes the result channel.
	CloseResultChannel()
}

// New creates a new job with the provided data.
func newJob[T, R any](data T, configs jobConfigs) *job[T, R] {
	return &job[T, R]{
		id:            configs.Id,
		Input:         data,
		resultChannel: NewResultChannel[R](1),
		status:        atomic.Uint32{},
		Output:        new(Result[R]),
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
	case created:
		return "Created"
	case queued:
		return "Queued"
	case processing:
		return "Processing"
	case finished:
		return "Finished"
	case closed:
		return "Closed"
	default:
		return "Unknown"
	}
}

// IsClosed returns true if the job has been closed.
func (j *job[T, R]) IsClosed() bool {
	return j.status.Load() == closed
}

// ChangeStatus updates the job's status to the provided value.
func (j *job[T, R]) ChangeStatus(s status) iJob[T, R] {
	j.status.Store(s)
	return j
}

// SaveAndSendResult sends a result to the job's result channel.
func (j *job[T, R]) SaveAndSendResult(result R) {
	r := Result[R]{Data: result}
	j.Output = &r
	j.resultChannel.Send(r)
}

// SaveAndSendError sends an error to the job's result channel.
func (j *job[T, R]) SaveAndSendError(err error) {
	r := Result[R]{Err: err}
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
	ch, err := j.resultChannel.Read()

	if ch != nil {
		return err
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
	case processing:
		return errors.New("job is processing, you can't close processing job")
	case closed:
		return errors.New("job is already closed")
	}

	return nil
}

func (j *job[T, R]) Json() ([]byte, error) {
	view := jobView[T, R]{
		Id:       j.ID(),
		Status:   j.Status(),
		Input:    j.Input,
		Output:   j.Output,
		Priority: j.priority,
	}

	return json.Marshal(view)
}

func parseToJob[T, R any](data []byte) (iJob[T, R], error) {
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
		j.status.Store(created)
	case "Queued":
		j.status.Store(queued)
	case "Processing":
		j.status.Store(processing)
	case "Finished":
		j.status.Store(finished)
	case "Closed":
		j.status.Store(closed)
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
	j.status.Store(closed)

	return nil
}
