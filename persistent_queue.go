package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
)

type ConcurrentPersistentQueue[T, R any] interface {
	ICQueue[R]
	// Add adds a new Job to the queue and returns a channel to receive the result.
	// Time complexity: O(1)
	Add(data T, id ...string) types.EnqueuedJob[R]
	// AddAll adds multiple Jobs to the queue and returns a channel to receive all responses.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) types.EnqueuedGroupJob[R]
}

type concurrentPersistentQueue[T, R any] struct {
	*concurrentQueue[T, R]
}

func newPersistentQueue[T, R any](w Worker[T, R]) ConcurrentPersistentQueue[T, R] {
	return &concurrentPersistentQueue[T, R]{concurrentQueue: newQueue[T, R](w)}
}

func (q *concurrentPersistentQueue[T, R]) Add(data T, id ...string) types.EnqueuedJob[R] {
	j := job.New[T, R](data, id...)
	val, _ := j.Json()

	q.queue().Enqueue(val)
	q.postEnqueue(j)

	return j
}

func (q *concurrentPersistentQueue[T, R]) AddAll(items []Item[T]) types.EnqueuedGroupJob[R] {
	groupJob := job.NewGroupJob[T, R](uint32(len(items)))

	q.syncGroup().wg.Add(len(items))
	for _, item := range items {
		j := groupJob.NewJob(item.Value, item.ID)
		val, _ := j.Json()

		q.queue().Enqueue(val)
		q.postEnqueue(j)
	}

	return groupJob
}
