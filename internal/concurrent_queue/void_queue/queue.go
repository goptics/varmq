package void_queue

import (
	"sync"

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
	AddAll(items []T) <-chan error
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
		Data: data,
		ResultChannel: &job.ResultChannel[any]{
			Err: make(chan error, 1),
		},
	}

	q.AddJob(queue.EnqItem[*job.Job[T, any]]{Value: j})

	return j
}

func (q *ConcurrentVoidQueue[T]) AddAll(data []T) <-chan error {
	wg := sync.WaitGroup{}
	result := make(chan error, len(data))
	channel := job.NewVoidResultChannel()
	j := job.NewWithResultChannel[T, any](channel).Lock()

	go func() {
		for e := range channel.Err {
			result <- e
			wg.Done()
		}
	}()

	wg.Add(len(data))
	for _, item := range data {
		initJob := j.Init(item)

		q.AddJob(queue.EnqItem[*job.Job[T, any]]{Value: initJob})
	}

	go func() {
		wg.Wait()

		channel.Close()
		close(result)
	}()

	return result
}
