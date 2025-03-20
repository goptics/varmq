package gocq

import (
	"sync"

	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
)

type ConcurrentPersistentQueue[T, R any] interface {
	ICQueue[R]
	// WithCache sets the cache for the queue.
	WithCache(cache Cache) ConcurrentPersistentQueue[T, R]
	// Pause pauses the processing of jobs.
	Pause() ConcurrentPersistentQueue[T, R]
	// Add adds a new Job to the queue and returns a channel to receive the result.
	// Time complexity: O(1)
	Add(data T, id ...string) types.EnqueuedJob[R]
	// AddAll adds multiple Jobs to the queue and returns a channel to receive all responses.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) types.EnqueuedGroupJob[R]

	SetWorker(w Worker[T, R]) error
}

type concurrentPersistentQueue[T, R any] struct {
	*concurrentQueue[T, R]
}

func NewPersistentQueue[T, R any](concurrency uint32, persistentQ queue.IQueue) ConcurrentPersistentQueue[T, R] {
	return &concurrentPersistentQueue[T, R]{concurrentQueue: &concurrentQueue[T, R]{
		Concurrency:     concurrency,
		ChannelsStack:   make([]chan job.Job[T, R], concurrency),
		JobQueue:        persistentQ,
		jobCache:        new(sync.Map),
		jobPullNotifier: job.NewNotifier(),
	}}
}

func (q *concurrentPersistentQueue[T, R]) WithCache(cache Cache) ConcurrentPersistentQueue[T, R] {
	q.jobCache = cache
	return q
}

func (q *concurrentPersistentQueue[T, R]) Pause() ConcurrentPersistentQueue[T, R] {
	q.pause()
	return q
}

func (q *concurrentPersistentQueue[T, R]) SetWorker(w Worker[T, R]) error {
	q.PauseAndWait()

	q.mx.Lock()
	q.concurrentQueue.Worker = w
	q.mx.Unlock()
	err := q.start()

	q.Resume()

	return err
}

func (q *concurrentPersistentQueue[T, R]) Add(data T, id ...string) types.EnqueuedJob[R] {
	j := job.New[T, R](data, id...)
	val, _ := j.Json()

	q.wg.Add(1)
	q.JobQueue.Enqueue(val)
	q.postEnqueue(j)

	return j
}

func (q *concurrentPersistentQueue[T, R]) Close() error {
	q.stop()
	q.JobQueue.Close()
	return nil
}
