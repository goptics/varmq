package varmq

import (
	"io"
)

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
	// It returns false when the queue is full or closed, or while a job with the
	// same id (WithJobId or the worker's generator) is in the queue.
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) (EnqueuedJob, bool)
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedGroupJob to handle the job.
	// An item that does not fit, or whose id is in the queue, is skipped. Item
	// ids get a "g:" prefix, so item "a" and an Add with id "a" do not collide.
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
func newQueue[T any](w *worker[T, iJob[T]], q IQueue, configs ...QueueConfigFunc) *queue[T] {
	c := loadQueueConfigs(configs...)
	src := w.bind(q, c.priority)

	return &queue[T]{
		externalBaseQueue: newExternalQueue(q, w, c, purgeStateOf(src)),
		internalQueue:     q,
	}
}

func (q *queue[T]) Add(data T, configs ...JobConfigFunc) (EnqueuedJob, bool) {
	j := newJob(data, loadJobConfigs(q.w.configs(), configs...))

	if ok := enqueue(q.internalQueue, j, j.id); !ok {
		j.Close()
		return nil, false
	}

	q.w.Metrics().incSubmitted()
	j.markQueued()
	q.w.notifyToPullNextJobs()

	return j, true
}

func (q *queue[T]) AddAll(items []Item[T]) EnqueuedGroupJob {
	groupJob := newGroupJob[T](len(items))

	cfg := q.w.configs()

	for _, item := range items {
		j := groupJob.newJob(item.Data, cfg.jobConfigsFor(item.ID))
		if ok := enqueue(q.internalQueue, j, j.id); !ok {
			j.Close()
			continue
		}

		q.w.Metrics().incSubmitted()
		j.markQueued()
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
	// It returns false when the queue is full or closed, or while a job with the
	// same id (WithJobId or the worker's generator) is in the queue.
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) (EnqueuedErrJob, bool)
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedErrGroupJob to handle the job with error receiving.
	// An item that does not fit, or whose id is in the queue, is skipped. Item
	// ids get a "g:" prefix, so item "a" and an Add with id "a" do not collide.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedErrGroupJob
}

func newErrorQueue[T any](w *worker[T, iErrorJob[T]], q IQueue, configs ...QueueConfigFunc) *errorQueue[T] {
	c := loadQueueConfigs(configs...)
	src := w.bind(q, c.priority)

	return &errorQueue[T]{
		externalBaseQueue: newExternalQueue(q, w, c, purgeStateOf(src)),
		internalQueue:     q,
	}
}

func (q *errorQueue[T]) Add(data T, configs ...JobConfigFunc) (EnqueuedErrJob, bool) {
	j := newErrorJob(data, loadJobConfigs(q.w.configs(), configs...))

	if ok := enqueue(q.internalQueue, j, j.id); !ok {
		j.Close()
		return nil, false
	}

	q.w.Metrics().incSubmitted()
	j.markQueued()
	q.w.notifyToPullNextJobs()

	return j, true
}

func (q *errorQueue[T]) AddAll(items []Item[T]) EnqueuedErrGroupJob {
	groupJob := newErrorGroupJob[T](len(items))

	cfg := q.w.configs()

	for _, item := range items {
		j := groupJob.newJob(item.Data, cfg.jobConfigsFor(item.ID))
		if ok := enqueue(q.internalQueue, j, j.id); !ok {
			j.Close()
			continue
		}

		q.w.Metrics().incSubmitted()
		j.markQueued()
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
	// It returns false when the queue is full or closed, or while a job with the
	// same id (WithJobId or the worker's generator) is in the queue.
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) (EnqueuedResultJob[R], bool)
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedResultGroupJob to handle the job with result receiving.
	// An item that does not fit, or whose id is in the queue, is skipped. Item
	// ids get a "g:" prefix, so item "a" and an Add with id "a" do not collide.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedResultGroupJob[R]
}

func newResultQueue[T, R any](w *worker[T, iResultJob[T, R]], q IQueue, configs ...QueueConfigFunc) *resultQueue[T, R] {
	c := loadQueueConfigs(configs...)
	src := w.bind(q, c.priority)

	return &resultQueue[T, R]{
		externalBaseQueue: newExternalQueue(q, w, c, purgeStateOf(src)),
		internalQueue:     q,
	}
}

func (q *resultQueue[T, R]) Add(data T, configs ...JobConfigFunc) (EnqueuedResultJob[R], bool) {
	j := newResultJob[T, R](data, loadJobConfigs(q.w.configs(), configs...))

	if ok := enqueue(q.internalQueue, j, j.id); !ok {
		j.Close()
		return nil, false
	}

	q.w.Metrics().incSubmitted()
	j.markQueued()
	q.w.notifyToPullNextJobs()

	return j, true
}

func (q *resultQueue[T, R]) AddAll(items []Item[T]) EnqueuedResultGroupJob[R] {
	groupJob := newResultGroupJob[T, R](len(items))

	cfg := q.w.configs()

	for _, item := range items {
		j := groupJob.newJob(item.Data, cfg.jobConfigsFor(item.ID))
		if ok := enqueue(q.internalQueue, j, j.id); !ok {
			j.Close()
			continue
		}

		q.w.Metrics().incSubmitted()
		j.markQueued()
		q.w.notifyToPullNextJobs()
	}

	return groupJob
}

type externalBaseQueue struct {
	w      Worker
	q      IBaseQueue
	config queueConfig
	purge  *purgeState // what Purge drives for the bound source; nil if it never batches
}

type IExternalBaseQueue interface {
	// Deprecated: Use Queue's Len() method instead for a more idiomatic approach.
	// This method will be removed in an upcoming release.
	NumPending() int

	// Len returns the number of jobs in the queue. Jobs a worker has already
	// taken into a batch but not started are not in the queue; the worker's
	// NumPending counts them.
	Len() int
	// IsFull returns true if the queue has reached its configured capacity.
	// If no capacity is set, it always returns false. Capacity bounds the
	// jobs in storage: jobs a worker took in a batch, or parked on Stop or
	// Pause, are outside it, at most 32 per worker.
	IsFull() bool
	// Purge removes all pending Jobs from the queue.
	Purge()
	// Close closes the queue, no more jobs can be added to the queue after closing the queue.
	Close() error
}

func newExternalQueue(q IBaseQueue, worker Worker, config queueConfig, purge *purgeState) *externalBaseQueue {
	if cap, ok := q.(CapacitySetter); ok {
		cap.SetCapacity(config.capacity)
	}

	return &externalBaseQueue{
		w:      worker,
		q:      q,
		config: config,
		purge:  purge,
	}
}

func (eq *externalBaseQueue) IsFull() bool {
	return eq.config.capacity > 0 && eq.q.Len() >= eq.config.capacity
}

func (eq *externalBaseQueue) NumPending() int {
	return eq.q.Len()
}

func (eq *externalBaseQueue) Len() int {
	return eq.q.Len()
}

func (eq *externalBaseQueue) Worker() Worker {
	return eq.w
}

// Purge closes every job not started yet: the ones in storage and the ones
// a worker took into its batch. Bumping the counter first makes the worker
// close those, as it reaches each one.
func (eq *externalBaseQueue) Purge() {
	eq.bumpPurges()
	prevValues := eq.q.Values()
	eq.q.Purge()

	// close all pending channels to avoid routine leaks
	for _, val := range prevValues {
		if j, ok := val.(io.Closer); ok {
			j.Close()
		}
	}
}

// bumpPurges makes every worker bound to this storage close, not run, the
// jobs it took from it and has not started: those in a batch as it reaches
// them, and those parked by a stop or pause at once.
func (eq *externalBaseQueue) bumpPurges() {
	if eq.purge != nil {
		eq.purge.purge()
	}
}

func (eq *externalBaseQueue) Close() error {
	return eq.q.Close()
}
