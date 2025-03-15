package void_queue

import (
	cq "github.com/fahimfaisaal/gocq/internal/concurrent_queue"
	"github.com/fahimfaisaal/gocq/internal/job"
	"github.com/fahimfaisaal/gocq/internal/queue"
)

type ConcurrentVoidQueue[T any] struct {
	*cq.ConcurrentQueue[T, any]
}

type IConcurrentVoidQueue[T any] interface {
	cq.ICQueue[T, any]
	Pause() IConcurrentVoidQueue[T]
	Add(data T) cq.EnqueuedVoidJob
	AddAll(items []T) cq.EnqueuedVoidGroupJob
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and worker function.
func NewQueue[T any](concurrency uint32, worker cq.VoidWorker[T]) *ConcurrentVoidQueue[T] {
	concurrentQueue := &cq.ConcurrentQueue[T, any]{
		Concurrency:   concurrency,
		Worker:        worker,
		ChannelsStack: make([]chan *job.Job[T, any], concurrency),
		JobQueue:      queue.NewPriorityQueue[*job.Job[T, any]](),
	}

	concurrentQueue.Restart()
	return &ConcurrentVoidQueue[T]{
		ConcurrentQueue: concurrentQueue,
	}
}

// Pause pauses the processing of jobs.
func (q *ConcurrentVoidQueue[T]) Pause() IConcurrentVoidQueue[T] {
	q.PauseQueue()
	return q
}

// Add adds a new Job to the queue.
func (q *ConcurrentVoidQueue[T]) Add(data T) cq.EnqueuedVoidJob {
	j := &job.Job[T, any]{
		Data:          data,
		ResultChannel: job.NewVoidResultChannel(),
	}

	q.AddJob(queue.EnqItem[*job.Job[T, any]]{Value: j})

	return j
}

func (q *ConcurrentVoidQueue[T]) AddAll(data []T) cq.EnqueuedVoidGroupJob {
	groupJob := job.NewGroupVoidJob[T](q.Concurrency).FanInVoidResult(len(data))

	for _, item := range data {
		q.AddJob(queue.EnqItem[*job.Job[T, any]]{Value: groupJob.NewJob(item).Lock()})
	}

	return groupJob
}
