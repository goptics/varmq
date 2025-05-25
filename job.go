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
	ID() string
}

type StatusProvider interface {
	IsClosed() bool
	Status() string
}

type Awaitable interface {
	Wait()
}

type Drainer interface {
	Drain() error
}

// job represents a task to be executed by a worker. It maintains the task's
// current status, Payload data, and channels for receiving results.
type job[T any] struct {
	id      string
	payload T
	status  atomic.Uint32
	wg      sync.WaitGroup
	queue   IBaseQueue
	ackId   string
}

// jobView represents a view of a job's state for serialization.
type jobView[T any] struct {
	Id      string `json:"id"`
	Status  string `json:"status"`
	Payload T      `json:"payload"`
}

type Job[T any] interface {
	Identifiable
	Payload() T
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

type EnqueuedJob interface {
	Identifiable
	StatusProvider
	Awaitable
}

// New creates a new job with the provided data.
func newJob[T any](data T, configs jobConfigs) *job[T] {
	j := &job[T]{
		id:      configs.Id,
		payload: data,
		status:  atomic.Uint32{},
		wg:      sync.WaitGroup{},
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

func (j *job[T]) Payload() T {
	return j.payload
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
		Payload: j.payload,
	}

	return json.Marshal(view)
}

func parseToJob[T any](data []byte) (any, error) {
	var view jobView[T]
	if err := json.Unmarshal(data, &view); err != nil {
		return nil, fmt.Errorf("failed to parse job: %w", err)
	}

	j := &job[T]{
		id:      view.Id,
		payload: view.Payload,
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

type iResultJob[T, R any] interface {
	iJob[T]
	saveAndSendResult(result R)
	saveAndSendError(err error)
}

type resultJob[T, R any] struct {
	job[T]
	*helpers.Response[Result[R]]
}

type EnqueuedResultJob[R any] interface {
	EnqueuedJob
	Drainer
	Result() (R, error)
}

func newResultJob[T, R any](payload T, configs jobConfigs) *resultJob[T, R] {
	r := &resultJob[T, R]{
		job: job[T]{
			id:      configs.Id,
			payload: payload,
			status:  atomic.Uint32{},
			wg:      sync.WaitGroup{},
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

// saveAndSendResult saves the result and sends it to the job's result channel.
func (rj *resultJob[T, R]) saveAndSendResult(result R) {
	rj.Response.Send(Result[R]{JobId: rj.id, Data: result})
}

// saveAndSendError sends an error to the job's result channel.
func (rj *resultJob[T, R]) saveAndSendError(err error) {
	rj.Response.Send(Result[R]{JobId: rj.id, Err: err})
}

func (rj *resultJob[T, R]) Close() error {
	if err := rj.job.Close(); err != nil {
		return err
	}

	rj.Response.Close()
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

type EnqueuedErrJob interface {
	EnqueuedJob
	Drainer
	Err() error
}

func newErrorJob[T any](payload T, configs jobConfigs) *errorJob[T] {
	e := &errorJob[T]{
		job: job[T]{
			id:      configs.Id,
			payload: payload,
			status:  atomic.Uint32{},
			wg:      sync.WaitGroup{},
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
