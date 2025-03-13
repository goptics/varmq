package void_queue

import (
	"sync"

	cq "github.com/fahimfaisaal/gocq/internal/concurrent_queue"
	"github.com/fahimfaisaal/gocq/internal/job"
	"github.com/fahimfaisaal/gocq/internal/queue"
)

type ConcurrentVoidPriorityQueue[T any] struct {
	*cq.ConcurrentPriorityQueue[T, any]
}

type IConcurrentVoidPriorityQueue[T any] interface {
	cq.IQueue[T, any]
	Pause() IConcurrentVoidPriorityQueue[T]
	Add(data T, priority int) cq.EnqueuedVoidJob
	AddAll(items []cq.PQItem[T]) <-chan error
}

// Creates a new ConcurrentVoidPriorityQueue with the specified concurrency and worker function.
func NewPriorityQueue[T any](concurrency uint32, worker cq.VoidWorker[T]) *ConcurrentVoidPriorityQueue[T] {
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

// Add adds a new Job with the given priority to the queue.
func (q *ConcurrentVoidPriorityQueue[T]) Add(data T, priority int) cq.EnqueuedVoidJob {
	j := &job.Job[T, any]{
		Data: data,
		ResultChannel: &job.ResultChannel[any]{
			Err: make(chan error, 1),
		},
	}

	q.AddJob(j, queue.EnqItem[*job.Job[T, any]]{Value: j, Priority: priority})

	return j
}

// AddAll adds multiple Jobs with the given items to the queue and returns a channel to receive all error responses.
func (q *ConcurrentVoidPriorityQueue[T]) AddAll(items []cq.PQItem[T]) <-chan error {
	wg := sync.WaitGroup{}
	response := make(chan error, len(items))
	err := make(chan error, 1)
	channel := &job.ResultChannel[any]{
		Err: err,
	}

	go func(err <-chan error) {
		for e := range err {
			response <- e
			wg.Done()
		}
	}(err)

	wg.Add(len(items))
	for _, item := range items {
		j := &job.Job[T, any]{
			Data:          item.Value,
			ResultChannel: channel,
			Lock:          true,
		}

		q.AddJob(j, queue.EnqItem[*job.Job[T, any]]{Value: j, Priority: item.Priority})
	}

	go func() {
		wg.Wait()

		channel.Close()
		close(response)
	}()

	return response
}

// Pause pauses the processing of jobs.
func (q *ConcurrentVoidPriorityQueue[T]) Pause() IConcurrentVoidPriorityQueue[T] {
	q.PauseQueue()
	return q
}
