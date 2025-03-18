package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/types"
)

type concurrentPriorityQueue[T, R any] struct {
	*concurrentQueue[T, R]
}

type PQItem[T any] struct {
	ID string
	// Value contains the actual data stored in the queue
	Value T
	// Priority determines the item's position in the queue
	Priority int
}

type ConcurrentPriorityQueue[T, R any] interface {
	ICQueue[R]
	// WithCache sets the cache for the queue.
	WithCache(cache Cache) ConcurrentPriorityQueue[T, R]
	// Pause pauses the processing of jobs.
	Pause() ConcurrentPriorityQueue[T, R]
	// Add adds a new Job with the given priority to the queue and returns a channel to receive the result.
	// Time complexity: O(log n)
	Add(data T, priority int, id ...string) types.EnqueuedJob[R]
	// AddAll adds multiple Jobs with the given priority to the queue and returns a channel to receive all responses.
	// Time complexity: O(n log n) where n is the number of Jobs added
	AddAll(items []PQItem[T]) types.EnqueuedGroupJob[R]
}

// NewPriorityQueue creates a new concurrentPriorityQueue with the specified concurrency and worker function.
func newPriorityQueue[T, R any](concurrency uint32, worker any) *concurrentPriorityQueue[T, R] {
	concurrentQueue := &concurrentQueue[T, R]{
		Concurrency:   concurrency,
		Worker:        worker,
		ChannelsStack: make([]chan job.Job[T, R], concurrency),
		JobQueue:      queue.NewPriorityQueue[job.Job[T, R]](),
		jobCache:      getCache(),
	}

	concurrentQueue.Restart()
	return &concurrentPriorityQueue[T, R]{concurrentQueue: concurrentQueue}
}

func (q *concurrentPriorityQueue[T, R]) Pause() ConcurrentPriorityQueue[T, R] {
	q.PauseQueue()
	return q
}

func (q *concurrentPriorityQueue[T, R]) WithCache(cache Cache) ConcurrentPriorityQueue[T, R] {
	q.jobCache = cache
	return q
}

func (q *concurrentPriorityQueue[T, R]) Add(data T, priority int, id ...string) types.EnqueuedJob[R] {
	j := job.New[T, R](data, id...)

	q.AddJob(queue.EnqItem[job.Job[T, R]]{Value: j, Priority: priority})

	return j
}

func (q *concurrentPriorityQueue[T, R]) AddAll(items []PQItem[T]) types.EnqueuedGroupJob[R] {
	groupJob := job.NewGroupJob[T, R](uint32(len(items)))

	for _, item := range items {
		q.AddJob(queue.EnqItem[job.Job[T, R]]{Value: groupJob.NewJob(item.Value, item.ID), Priority: item.Priority})
	}

	return groupJob
}
