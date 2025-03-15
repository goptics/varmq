package concurrent_queue

import (
	"github.com/fahimfaisaal/gocq/internal/job"
	"github.com/fahimfaisaal/gocq/internal/queue"
	"github.com/fahimfaisaal/gocq/types"
)

type ConcurrentPriorityQueue[T, R any] struct {
	*ConcurrentQueue[T, R]
}

// NewPriorityQueue creates a new ConcurrentPriorityQueue with the specified concurrency and worker function.
func NewPriorityQueue[T, R any](concurrency uint32, worker types.Worker[T, R]) *ConcurrentPriorityQueue[T, R] {
	concurrentQueue := &ConcurrentQueue[T, R]{
		Concurrency:   concurrency,
		Worker:        worker,
		ChannelsStack: make([]chan *job.Job[T, R], concurrency),
		JobQueue:      queue.NewPriorityQueue[*job.Job[T, R]](),
	}

	concurrentQueue.Restart()
	return &ConcurrentPriorityQueue[T, R]{ConcurrentQueue: concurrentQueue}
}

func (q *ConcurrentPriorityQueue[T, R]) Pause() types.IConcurrentPriorityQueue[T, R] {
	q.PauseQueue()
	return q
}

func (q *ConcurrentPriorityQueue[T, R]) Add(data T, priority int) types.EnqueuedJob[R] {
	j := job.New[T, R](data)

	q.AddJob(queue.EnqItem[*job.Job[T, R]]{Value: j, Priority: priority})

	return j
}

func (q *ConcurrentPriorityQueue[T, R]) AddAll(items []types.PQItem[T]) types.EnqueuedGroupJob[R] {
	groupJob := job.NewGroupJob[T, R](q.Concurrency).FanInResult(len(items))

	for _, item := range items {
		q.AddJob(queue.EnqItem[*job.Job[T, R]]{Value: groupJob.NewJob(item.Value).Lock(), Priority: item.Priority})
	}

	return groupJob
}
