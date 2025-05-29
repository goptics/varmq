package varmq

import "io"

// queue is the base implementation of the Queue interface
// It contains an externalBaseQueue for worker management and an internalQueue for job storage
type queue[T any] struct {
	*externalBaseQueue
	internalQueue IQueue
}

type Queue[T any] interface {
	IExternalBaseQueue

	// Worker returns the bound worker.
	Worker() Worker
	// Add adds a new Job to the queue and returns a EnqueuedJob to handle the job.
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) (EnqueuedJob, bool)
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedGroupJob to handle the job.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedGroupJob
}

// Item represents a data item to be processed by a worker
// It combines a unique identifier with a value of any generic type
// and a optional priority for priority queues
// This only used for AddAll operations
type Item[T any] struct {
	ID       string
	Data     T
	Priority int
}

// newQueue creates a new queue with the given worker and internal queue implementation
// It sets the worker's queue to the provided queue and creates a new external queue for job management
func newQueue[T any](worker *worker[T, iJob[T]], q IQueue) *queue[T] {
	worker.setQueue(q)

	return &queue[T]{
		externalBaseQueue: newExternalQueue(q, worker),
		internalQueue:     q,
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
		j := groupJob.newJob(item.Data, loadJobConfigs(q.w.configs(), WithJobId(item.ID)))
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
	*externalBaseQueue
	internalQueue IQueue
}

type ResultQueue[T, R any] interface {
	IExternalBaseQueue
	// Worker returns the bound worker.
	Worker() Worker
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
		externalBaseQueue: newExternalQueue(q, worker),
		internalQueue:     q,
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
		j := groupJob.newJob(item.Data, loadJobConfigs(q.w.configs(), WithJobId(item.ID)))
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
	*externalBaseQueue
	internalQueue IQueue
}

type ErrQueue[T any] interface {
	IExternalBaseQueue

	// Worker returns the bound worker.
	Worker() Worker
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
		externalBaseQueue: newExternalQueue(q, worker),
		internalQueue:     q,
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
		j := groupJob.newJob(item.Data, loadJobConfigs(q.w.configs(), WithJobId(item.ID)))
		if ok := q.internalQueue.Enqueue(j); !ok {
			j.Close()
			continue
		}

		j.changeStatus(queued)
		q.w.notifyToPullNextJobs()
	}

	return groupJob
}

type externalBaseQueue struct {
	w Worker
	q IBaseQueue
}

type IExternalBaseQueue interface {
	PendingTracker
	// Purge removes all pending Jobs from the queue.
	Purge()
	// Close closes the queue and resets all internal states.
	Close() error
}

func newExternalQueue(q IBaseQueue, worker Worker) *externalBaseQueue {
	return &externalBaseQueue{
		w: worker,
		q: q,
	}
}

func (eq *externalBaseQueue) NumPending() int {
	return eq.q.Len()
}

func (eq *externalBaseQueue) Worker() Worker {
	return eq.w
}

func (eq *externalBaseQueue) Purge() {
	prevValues := eq.q.Values()
	eq.q.Purge()

	// close all pending channels to avoid routine leaks
	for _, val := range prevValues {
		if j, ok := val.(io.Closer); ok {
			j.Close()
		}
	}
}

func (eq *externalBaseQueue) Close() error {
	return eq.q.Close()
}
