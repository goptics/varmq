package concurrent_queue

import (
	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/types"
)

type ConcurrentPriorityQueue[T, R any] struct {
	*ConcurrentQueue[T, R]
}

// NewPriorityQueue creates a new ConcurrentPriorityQueue with the specified concurrency and worker function.
func NewPriorityQueue[T, R any](concurrency uint32, worker any) *ConcurrentPriorityQueue[T, R] {
	concurrentQueue := &ConcurrentQueue[T, R]{
		Concurrency:   concurrency,
		Worker:        worker,
		ChannelsStack: make([]chan job.IJob[T, R], concurrency),
		JobQueue:      queue.NewPriorityQueue[job.IJob[T, R]](),
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

	q.AddJob(queue.EnqItem[job.IJob[T, R]]{Value: j, Priority: priority})

	return j
}

func (q *ConcurrentPriorityQueue[T, R]) AddAll(items []types.PQItem[T]) types.EnqueuedGroupJob[R] {
	groupJob := job.NewGroupJob[T, R](uint32(len(items)))

	for _, item := range items {
		q.AddJob(queue.EnqItem[job.IJob[T, R]]{Value: groupJob.NewJob(item.Value), Priority: item.Priority})
	}

	return groupJob
}
