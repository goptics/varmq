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
	// Pause pauses the processing of jobs.
	Pause() IConcurrentVoidQueue[T]
	// Add adds a new Job to the queue and returns a EnqueuedVoidJob to handle the void job.
	// Time complexity: O(1)
	Add(data T) cq.EnqueuedVoidJob
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedVoidGroupJob to handle the job.
	// Time complexity: O(n) where n is the number of Jobs added
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

func (q *ConcurrentVoidQueue[T]) Pause() IConcurrentVoidQueue[T] {
	q.PauseQueue()
	return q
}

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
