package void_queue

import (
	cq "github.com/fahimfaisaal/gocq/v2/internal/concurrent_queue"
	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/types"
)

type ConcurrentVoidPriorityQueue[T any] struct {
	*cq.ConcurrentPriorityQueue[T, any]
}

// Creates a new ConcurrentVoidPriorityQueue with the specified concurrency and worker function.
func NewPriorityQueue[T any](concurrency uint32, worker types.VoidWorker[T]) *ConcurrentVoidPriorityQueue[T] {
	concurrentQueue := &cq.ConcurrentQueue[T, any]{
		Concurrency:   concurrency,
		Worker:        worker,
		ChannelsStack: make([]chan *job.Job[T, any], concurrency),
		JobQueue:      queue.NewPriorityQueue[*job.Job[T, any]](),
	}

	concurrentQueue.Restart()
	return &ConcurrentVoidPriorityQueue[T]{
		ConcurrentPriorityQueue: &cq.ConcurrentPriorityQueue[T, any]{
			ConcurrentQueue: concurrentQueue,
		},
	}
}

func (q *ConcurrentVoidPriorityQueue[T]) Pause() types.IConcurrentVoidPriorityQueue[T] {
	q.PauseQueue()
	return q
}

func (q *ConcurrentVoidPriorityQueue[T]) Add(data T, priority int) types.EnqueuedVoidJob {
	j := &job.Job[T, any]{
		Data:          data,
		ResultChannel: job.NewVoidResultChannel(),
	}

	q.AddJob(queue.EnqItem[*job.Job[T, any]]{Value: j, Priority: priority})

	return j
}

func (q *ConcurrentVoidPriorityQueue[T]) AddAll(items []types.PQItem[T]) types.EnqueuedVoidGroupJob {
	groupJob := job.NewGroupVoidJob[T](q.Concurrency).FanInVoidResult(len(items))

	for _, item := range items {
		q.AddJob(queue.EnqItem[*job.Job[T, any]]{Value: groupJob.NewJob(item.Value).Lock(), Priority: item.Priority})
	}

	return groupJob
}
