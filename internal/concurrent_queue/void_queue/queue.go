package void_queue

import (
	cq "github.com/fahimfaisaal/gocq/v2/internal/concurrent_queue"
	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/types"
)

type ConcurrentVoidQueue[T any] struct {
	*cq.ConcurrentQueue[T, any]
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and worker function.
func NewQueue[T any](concurrency uint32, worker types.VoidWorker[T]) *ConcurrentVoidQueue[T] {
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

func (q *ConcurrentVoidQueue[T]) Pause() types.IConcurrentVoidQueue[T] {
	q.PauseQueue()
	return q
}

func (q *ConcurrentVoidQueue[T]) Add(data T) types.EnqueuedVoidJob {
	j := &job.Job[T, any]{
		Data:          data,
		ResultChannel: job.NewVoidResultChannel(),
	}

	q.AddJob(queue.EnqItem[*job.Job[T, any]]{Value: j})

	return j
}

func (q *ConcurrentVoidQueue[T]) AddAll(data []T) types.EnqueuedVoidGroupJob {
	groupJob := job.NewGroupVoidJob[T](q.Concurrency).FanInVoidResult(len(data))

	for _, item := range data {
		q.AddJob(queue.EnqItem[*job.Job[T, any]]{Value: groupJob.NewJob(item).Lock()})
	}

	return groupJob
}
