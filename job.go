package varmq

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/goptics/varmq/internal/helpers"
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

// Result represents the result of a job, containing the data and any error that occurred.
type Result[T any] struct {
	JobId string
	Data  T
	Err   error
}

type Identifiable interface {
	// ID returns the unique identifier of the job.
	// Returns empty in case don't set any id by client
	ID() string
}

type StatusProvider interface {
	// IsClosed returns true if the job has been closed.
	IsClosed() bool
	// Status returns a string representation of the job's current lifecycle state.
	// Possible return values:
	// - "Created": Job has been created but not yet added to a queue
	// - "Queued": Job has been added to a queue and is waiting to be processed
	// - "Processing": Job is currently being processed by a worker
	// - "Finished": Job processing has completed successfully
	// - "Closed": Job has been manually closed and will not be processed
	Status() string
}

type Awaitable interface {
	// Wait blocks the caller until the job has completed processing.
	// This method can be used to synchronize execution flow with job completion.
	// If the job has already finished, this method returns immediately.
	Wait()
}

type Drainer interface {
	// Drain cleans up the job's internal communication channels and releases associated resources.
	// This should be called when the job is no longer needed.
	// After calling Drain, the job's result channels will be closed and cannot be used again.
	Drain()
}

// job represents a task to be executed by a worker. It maintains the task's
// current status, Payload data, and channels for receiving results.
type job[T any] struct {
	id     string
	data   T
	status atomic.Uint32
	wg     sync.WaitGroup
	queue  IBaseQueue
	ackId  string
}

// jobView represents a view of a job's state for serialization.
type jobView[T any] struct {
	Id      string `json:"id"`
	Status  string `json:"status"`
	Payload T      `json:"data"`
}

type Job[T any] interface {
	Identifiable
	// Data returns the data associated with the job.
	Data() T
}

type iJob[T any] interface {
	Job[T]
	StatusProvider
	changeStatus(s status)
	setAckId(id string)
	setInternalQueue(q IBaseQueue)
	ack() error
	io.Closer
}

// EnqueuedJob is an interface representing a job that has been successfully enqueued.
// It extends Identifiable, StatusProvider, and Awaitable interfaces to provide
// identification, status tracking, and waiting capabilities. This interface is
// returned by the Add method when used with NewWorker
type EnqueuedJob interface {
	Identifiable
	StatusProvider
	Awaitable
}

// New creates a new job with the provided data.
func newJob[T any](data T, configs jobConfigs) *job[T] {
	j := &job[T]{
		id:     configs.Id,
		data:   data,
		status: atomic.Uint32{},
		wg:     sync.WaitGroup{},
	}

	j.wg.Add(1)

	return j
}

func (j *job[T]) setAckId(id string) {
	j.ackId = id
}

func (j *job[T]) setInternalQueue(q IBaseQueue) {
	j.queue = q
}

func (j *job[T]) ID() string {
	return j.id
}

func (j *job[T]) Data() T {
	return j.data
}

// State returns the current status of the job as a string.
func (j *job[T]) Status() string {
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
func (j *job[T]) IsClosed() bool {
	return j.status.Load() == closed
}

// changeStatus updates the job's status to the provided value.
func (j *job[T]) changeStatus(s status) {
	j.status.Store(s)
}

func (j *job[T]) Wait() {
	j.wg.Wait()
}

func (j *job[T]) Json() ([]byte, error) {
	view := jobView[T]{
		Id:      j.ID(),
		Status:  j.Status(),
		Payload: j.data,
	}

	return json.Marshal(view)
}

func parseToJob[T any](data []byte) (any, error) {
	var view jobView[T]
	if err := json.Unmarshal(data, &view); err != nil {
		return nil, fmt.Errorf("failed to parse job: %w", err)
	}

	j := newJob(view.Payload, jobConfigs{
		Id: view.Id,
	})

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

func (j *job[T]) isCloseable() error {
	switch j.status.Load() {
	case processing:
		return errors.New("job is processing, you can't close processing job")
	case closed:
		return errors.New("job is already closed")
	}

	return nil
}

// close closes the job and its associated channels.
// the job regardless of its current state, except when locked.
func (j *job[T]) Close() error {
	if err := j.isCloseable(); err != nil {
		return err
	}

	j.ack()
	j.status.Store(closed)
	j.wg.Done()
	return nil
}

func (j *job[T]) ack() error {
	if j.ackId == "" || j.IsClosed() {
		return errors.New("job is not acknowledgeable")
	}

	if _, ok := j.queue.(IAcknowledgeable); !ok {
		return errors.New("job is not acknowledgeable")
	}

	if ok := j.queue.(IAcknowledgeable).Acknowledge(j.ackId); !ok {
		return fmt.Errorf("queue failed to acknowledge job %s (ackId=%s)", j.id, j.ackId)
	}

	return nil
}

type iErrorJob[T any] interface {
	iJob[T]
	sendError(err error)
}

type errorJob[T any] struct {
	job[T]
	*helpers.Response[error]
}

// EnqueuedErrJob is an interface representing an error job that has been successfully enqueued.
// It extends EnqueuedJob and Drainer interfaces to provide identification, status tracking,
// waiting, and cleanup capabilities. This interface is returned by the Add method when used
// with NewErrWorker, allowing clients to retrieve any error through the Err() method.
type EnqueuedErrJob interface {
	EnqueuedJob
	Drainer
	// Err retrieves any error that occurred during job execution.
	// This method blocks the caller until the job processing is complete.
	// If the job has already finished, it returns immediately with the error result.
	// A nil return value indicates successful job execution without errors.
	Err() error
}

func newErrorJob[T any](data T, configs jobConfigs) *errorJob[T] {
	e := &errorJob[T]{
		job: job[T]{
			id:     configs.Id,
			data:   data,
			status: atomic.Uint32{},
			wg:     sync.WaitGroup{},
		},
		Response: helpers.NewResponse[error](1),
	}
	e.wg.Add(1)
	return e
}

func (ej *errorJob[T]) Err() error {
	resErr, err := ej.Response.Response()

	if err != nil {
		return err
	}

	return resErr
}

func (ej *errorJob[T]) sendError(err error) {
	ej.Response.Send(err)
}

func (ej *errorJob[T]) Close() error {
	if err := ej.job.Close(); err != nil {
		return err
	}

	ej.Response.Close()
	return nil
}

type iResultJob[T, R any] interface {
	iErrorJob[T]
	sendResult(result R)
}

type resultJob[T, R any] struct {
	job[T]
	*helpers.Response[Result[R]]
}

// EnqueuedResultJob is an interface representing a result job that has been successfully enqueued.
// It extends EnqueuedJob and Drainer interfaces to provide identification, status tracking,
// waiting, and cleanup capabilities. This interface is returned by the Add method when used
// with NewResultWorker, allowing clients to retrieve the job result through the Result() method.
type EnqueuedResultJob[R any] interface {
	EnqueuedJob
	Drainer
	// Result retrieves the computed result value and any error from the job execution.
	// This method blocks the caller until the job processing is complete.
	// If the job has already finished, it returns immediately with the result.
	Result() (R, error)
	// Close marks the job as closed without removing it from the queue.
	// When a closed job is dequeued, the worker will skip processing it.
	// This operation takes constant time as it only updates the job's state.
	Close() error
}

func newResultJob[T, R any](data T, configs jobConfigs) *resultJob[T, R] {
	r := &resultJob[T, R]{
		job: job[T]{
			id:     configs.Id,
			data:   data,
			status: atomic.Uint32{},
			wg:     sync.WaitGroup{},
		},
		Response: helpers.NewResponse[Result[R]](1),
	}
	r.wg.Add(1)
	return r
}

func (rj *resultJob[T, R]) Result() (R, error) {
	result, err := rj.Response.Response()

	if err != nil {
		return *new(R), err
	}

	return result.Data, result.Err
}

// sendResult saves the result and sends it to the job's result channel.
func (rj *resultJob[T, R]) sendResult(result R) {
	rj.Response.Send(Result[R]{JobId: rj.id, Data: result})
}

// sendError sends an error to the job's result channel.
func (rj *resultJob[T, R]) sendError(err error) {
	rj.Response.Send(Result[R]{JobId: rj.id, Err: err})
}

func (rj *resultJob[T, R]) Close() error {
	if err := rj.job.Close(); err != nil {
		return err
	}

	rj.Response.Close()
	return nil
}
