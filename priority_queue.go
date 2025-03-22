package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
)

type concurrentPriorityQueue[T, R any] struct {
	*concurrentQueue[T, R]
	queue types.IPriorityQueue
}

type PQItem[T any] struct {
	ID string
	// Value contains the actual data stored in the queue
	Value T
	// Priority determines the item's position in the queue
	Priority int
}

type ConcurrentPriorityQueue[T, R any] interface {
	ICQueue[T, R]
	// Add adds a new Job with the given priority to the queue and returns a channel to receive the result.
	// Time complexity: O(log n)
	Add(data T, priority int, id ...string) types.EnqueuedJob[R]
	// AddAll adds multiple Jobs with the given priority to the queue and returns a channel to receive all responses.
	// Time complexity: O(n log n) where n is the number of Jobs added
	AddAll(items []PQItem[T]) types.EnqueuedGroupJob[R]
}

// NewPriorityQueue creates a new concurrentPriorityQueue with the specified concurrency and worker function.
func newPriorityQueue[T, R any](worker *worker[T, R]) *concurrentPriorityQueue[T, R] {
	q := queue.NewPriorityQueue[job.Job[T, R]]()

	worker.setQueue(q)

	return &concurrentPriorityQueue[T, R]{
		concurrentQueue: &concurrentQueue[T, R]{
			worker: worker,
		},
		queue: q,
	}
}

func (q *concurrentPriorityQueue[T, R]) Add(data T, priority int, id ...string) types.EnqueuedJob[R] {
	j := job.New[T, R](data, id...)

	q.queue.Enqueue(j, priority)
	q.sync.wg.Add(1)
	q.postEnqueue(j)

	return j
}

func (q *concurrentPriorityQueue[T, R]) AddAll(items []PQItem[T]) types.EnqueuedGroupJob[R] {
	l := len(items)
	groupJob := job.NewGroupJob[T, R](uint32(l))

	q.sync.wg.Add(l)
	for _, item := range items {
		j := groupJob.NewJob(item.Value, item.ID)

		q.queue.Enqueue(j, item.Priority)
		q.postEnqueue(j)
	}

	return groupJob
}
