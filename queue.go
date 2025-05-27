package varmq

import "io"

// queue is the base implementation of the Queue interface
// It contains an externalQueue for worker management and an internalQueue for job storage
type queue[T any] struct {
	*externalQueue
	internalQueue IQueue
}

type Queue[T any] interface {
	IExternalQueue
	// Add adds a new Job to the queue and returns a EnqueuedJob to handle the job.
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) (EnqueuedJob, bool)
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedGroupJob to handle the job.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedGroupJob
}

// Item represents a data item to be processed by a worker
// It combines a unique identifier with a value of any generic type
type Item[T any] struct {
	ID       string
	Value    T
	Priority int
}

// newQueue creates a new queue with the given worker and internal queue implementation
// It sets the worker's queue to the provided queue and creates a new external queue for job management
func newQueue[T any](worker *worker[T, iJob[T]], q IQueue) *queue[T] {
	worker.setQueue(q)

	return &queue[T]{
		externalQueue: newExternalQueue(worker),
		internalQueue: q,
	}
}

func (q *queue[T]) Add(data T, configs ...JobConfigFunc) (EnqueuedJob, bool) {
	j := newJob(data, loadJobConfigs(q.w.configs(), configs...))

	if ok := q.internalQueue.Enqueue(j); !ok {
		j.Close()
		return nil, false
	}

	j.changeStatus(queued)
	q.w.notifyToPullNextJobs()

	return j, true
}

func (q *queue[T]) AddAll(items []Item[T]) EnqueuedGroupJob {
	groupJob := newGroupJob[T](len(items))

	for _, item := range items {
		j := groupJob.newJob(item.Value, loadJobConfigs(q.w.configs(), WithJobId(item.ID)))
		if ok := q.internalQueue.Enqueue(j); !ok {
			j.Close()
			continue
		}

		j.changeStatus(queued)
		q.w.notifyToPullNextJobs()
	}

	return groupJob
}

type resultQueue[T, R any] struct {
	*externalQueue
	internalQueue IQueue
}

type ResultQueue[T, R any] interface {
	IExternalQueue
	// Add adds a new Job to the queue and returns a EnqueuedResultJob to handle the job with result receiving.
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) (EnqueuedResultJob[R], bool)
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedResultGroupJob to handle the job with result receiving.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedResultGroupJob[R]
}

func newResultQueue[T, R any](worker *worker[T, iResultJob[T, R]], q IQueue) *resultQueue[T, R] {
	worker.setQueue(q)

	return &resultQueue[T, R]{
		externalQueue: newExternalQueue(worker),
		internalQueue: q,
	}
}

func (q *resultQueue[T, R]) Add(data T, configs ...JobConfigFunc) (EnqueuedResultJob[R], bool) {
	j := newResultJob[T, R](data, loadJobConfigs(q.w.configs(), configs...))

	if ok := q.internalQueue.Enqueue(j); !ok {
		j.Close()
		return nil, false
	}

	j.changeStatus(queued)
	q.w.notifyToPullNextJobs()

	return j, true
}

func (q *resultQueue[T, R]) AddAll(items []Item[T]) EnqueuedResultGroupJob[R] {
	groupJob := newResultGroupJob[T, R](len(items))

	for _, item := range items {
		j := groupJob.newJob(item.Value, loadJobConfigs(q.w.configs(), WithJobId(item.ID)))
		if ok := q.internalQueue.Enqueue(j); !ok {
			j.Close()
			continue
		}

		j.changeStatus(queued)
		q.w.notifyToPullNextJobs()
	}

	return groupJob
}

type errorQueue[T any] struct {
	*externalQueue
	internalQueue IQueue
}

type ErrQueue[T any] interface {
	IExternalQueue
	// Add adds a new Job to the queue and returns a EnqueuedErrJob to handle the job with error receiving.
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) (EnqueuedErrJob, bool)
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedErrGroupJob to handle the job with error receiving.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedErrGroupJob
}

func newErrorQueue[T any](worker *worker[T, iErrorJob[T]], q IQueue) *errorQueue[T] {
	worker.setQueue(q)

	return &errorQueue[T]{
		externalQueue: newExternalQueue(worker),
		internalQueue: q,
	}
}

func (q *errorQueue[T]) Add(data T, configs ...JobConfigFunc) (EnqueuedErrJob, bool) {
	j := newErrorJob(data, loadJobConfigs(q.w.configs(), configs...))

	if ok := q.internalQueue.Enqueue(j); !ok {
		j.Close()
		return nil, false
	}

	j.changeStatus(queued)
	q.w.notifyToPullNextJobs()

	return j, true
}

func (q *errorQueue[T]) AddAll(items []Item[T]) EnqueuedErrGroupJob {
	groupJob := newErrorGroupJob[T](len(items))

	for _, item := range items {
		j := groupJob.newJob(item.Value, loadJobConfigs(q.w.configs(), WithJobId(item.ID)))
		if ok := q.internalQueue.Enqueue(j); !ok {
			j.Close()
			continue
		}

		j.changeStatus(queued)
		q.w.notifyToPullNextJobs()
	}

	return groupJob
}

type externalQueue struct {
	w Worker
}

type IExternalBaseQueue interface {
	PendingTracker
	// Purge removes all pending Jobs from the queue.
	Purge()
	// Close closes the queue and resets all internal states.
	Close() error
}

// IExternalQueue is the root interface of concurrent queue operations.
type IExternalQueue interface {
	IExternalBaseQueue

	// Worker returns the worker.
	Worker() Worker
	// WaitUntilFinished waits until all pending Jobs in the queue are processed.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitUntilFinished()
	// WaitAndClose waits until all pending Jobs in the queue are processed and then closes the queue.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitAndClose() error
}

func newExternalQueue(worker Worker) *externalQueue {
	return &externalQueue{
		w: worker,
	}
}

func (eq *externalQueue) NumPending() int {
	return eq.w.queue().Len()
}

func (eq *externalQueue) Worker() Worker {
	return eq.w
}

func (eq *externalQueue) WaitUntilFinished() {
	// to ignore deadlock error if the queue is paused
	if eq.w.IsPaused() {
		eq.w.Resume()
	}

	eq.w.wait()
}

func (eq *externalQueue) Purge() {
	prevValues := eq.w.queue().Values()
	eq.w.queue().Purge()

	// close all pending channels to avoid routine leaks
	for _, val := range prevValues {
		if j, ok := val.(io.Closer); ok {
			j.Close()
		}
	}
}

func (q *externalQueue) Close() error {
	q.Purge()
	if err := q.w.Stop(); err != nil {
		return err
	}
	q.WaitUntilFinished()

	return nil
}

func (q *externalQueue) WaitAndClose() error {
	q.WaitUntilFinished()
	return q.Close()
}
