package varmq

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
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

var (
	ErrJobProcessing    = errors.New("job is processing, you can't close processing job")
	ErrJobAlreadyClosed = errors.New("job is already closed")
	ErrAcknowledgeJob   = errors.New("queue failed to acknowledge job")
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
	// Drain discards the results or errors a group job's channel still holds,
	// so a caller that does not read them does not keep the channel full.
	// A single job keeps its result in place and has no channel: Drain is a
	// no-op there, and Err or Result still return the value.
	Drain()
}

// job represents a task to be executed by a worker. It maintains the task's
// current status, Payload data, and channels for receiving results.
type job[T any] struct {
	id     string
	data   T
	status atomic.Uint32
	wg     sync.WaitGroup
	ackRef *jobAck // nil for in-memory jobs; only ackSource.Next sets it
}

// jobAck is what a job needs to acknowledge itself: the storage that handed
// it out and the ack id that storage gave it. Folding both behind one pointer
// keeps job[T] in the 64-byte size class for the in-memory hot path.
type jobAck struct {
	q  IAcknowledgeable
	id string
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
	startProcessing() bool
	setAck(q IAcknowledgeable, id string)
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
	// Close marks the job as closed without removing it from the queue.
	// When a closed job is dequeued, the worker will skip processing it.
	// This operation takes constant time as it only updates the job's state.
	Close() error
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

// setAck attaches the storage and ack id that Close acknowledges with. An
// empty id means the storage has nothing to acknowledge, so nothing is kept.
// ponytail: one 32-byte allocation per acked job; that path already pays
// json.Unmarshal, so this is not pooled.
func (j *job[T]) setAck(q IAcknowledgeable, id string) {
	if id == "" {
		return
	}

	j.ackRef = &jobAck{q: q, id: id}
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
		return nil, fmt.Errorf("%w: %w", ErrParseJob, err)
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

// closeable reports why a job in state s cannot be closed, or nil.
func closeable(s status) error {
	switch s {
	case processing:
		return ErrJobProcessing
	case closed:
		return ErrJobAlreadyClosed
	}

	return nil
}

// claimClose moves a created, queued or finished job to closed with one CAS,
// so only one concurrent Close can win and run the close side effects.
func (j *job[T]) claimClose() error {
	for {
		s := j.status.Load()
		if err := closeable(s); err != nil {
			return err
		}

		if j.status.CompareAndSwap(s, closed) {
			return nil
		}
	}
}

// markQueued records that Enqueue succeeded. It only moves a created job: a
// worker may already have started it, or the user closed it, before this runs.
func (j *job[T]) markQueued() {
	j.status.CompareAndSwap(created, queued)
}

// startProcessing moves a job a worker took to processing, unless it was
// closed meanwhile.
func (j *job[T]) startProcessing() bool {
	for {
		s := j.status.Load()
		if s == closed {
			return false
		}

		if j.status.CompareAndSwap(s, processing) {
			return true
		}
	}
}

// Close closes the job, whatever its state except processing. Only the call
// that wins the transition releases waiters; a concurrent Close returns
// ErrJobAlreadyClosed. A job handed out by an acknowledgeable queue is
// acknowledged before the transition and never shows Closed meanwhile, so a
// failed acknowledgement leaves it untouched: a worker that took it still
// runs it, and a finished one stays Finished and unwaited. The price is that
// two simultaneous Close calls on such a job can both acknowledge.
func (j *job[T]) Close() error {
	if j.ackRef != nil {
		if err := closeable(j.status.Load()); err != nil {
			return err
		}

		if err := j.acknowledge(); err != nil {
			return err
		}
	}

	if err := j.claimClose(); err != nil {
		return err
	}

	j.wg.Done()

	return nil
}

func (j *job[T]) ack() error {
	if j.IsClosed() {
		return nil
	}

	return j.acknowledge()
}

func (j *job[T]) acknowledge() error {
	if j.ackRef == nil {
		return nil
	}

	if !j.ackRef.q.Acknowledge(j.ackRef.id) {
		return fmt.Errorf("%w: jobId=%s, ackId=%s", ErrAcknowledgeJob, j.id, j.ackRef.id)
	}

	return nil
}

type iErrorJob[T any] interface {
	iJob[T]
	sendError(err error)
}

// errorJob is a single job whose worker reports an error. The error is stored
// in place and published by the wg.Done in job.Close, so the job itself is the
// one-shot latch: no channel per job.
type errorJob[T any] struct {
	job[T]
	err error
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
	}
	e.wg.Add(1)
	return e
}

// Err blocks until the job is Closed (processed, purged or closed by the user)
// and returns the error sent before that, or nil.
func (ej *errorJob[T]) Err() error {
	ej.wg.Wait()
	return ej.err
}

// sendError stores the error. The worker calls it before Close, so the write
// happens-before the wg.Done that releases Err.
func (ej *errorJob[T]) sendError(err error) {
	ej.err = err
}

// Drain is a no-op: a single job has no channel to drain. It exists so the job
// satisfies the Drainer embedded in EnqueuedErrJob.
func (ej *errorJob[T]) Drain() {}

type iResultJob[T, R any] interface {
	iErrorJob[T]
	sendResult(result R)
}

// resultJob is a single job whose worker reports a result or an error, stored
// in place like errorJob.err. It holds the two fields rather than a Result[R]:
// Result's JobId would be a copy of job.id that Result() never returns, and
// without it the job stays in the 80-byte size class.
type resultJob[T, R any] struct {
	job[T]
	data R
	err  error
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
}

func newResultJob[T, R any](data T, configs jobConfigs) *resultJob[T, R] {
	r := &resultJob[T, R]{
		job: job[T]{
			id:     configs.Id,
			data:   data,
			status: atomic.Uint32{},
			wg:     sync.WaitGroup{},
		},
	}
	r.wg.Add(1)
	return r
}

// Result blocks until the job is Closed (processed, purged or closed by the
// user) and returns what was sent before that, or the zero value and nil.
func (rj *resultJob[T, R]) Result() (R, error) {
	rj.wg.Wait()
	return rj.data, rj.err
}

// sendResult stores the result. The worker calls it before Close, so the write
// happens-before the wg.Done that releases Result.
func (rj *resultJob[T, R]) sendResult(result R) {
	rj.data = result
}

// sendError stores the error, see sendResult.
func (rj *resultJob[T, R]) sendError(err error) {
	rj.err = err
}

// Drain is a no-op: a single job has no channel to drain. It exists so the job
// satisfies the Drainer embedded in EnqueuedResultJob.
func (rj *resultJob[T, R]) Drain() {}
