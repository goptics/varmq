package gocq

import (
	"sync"

	"github.com/fahimfaisaal/gocq/internal/job"
	"github.com/fahimfaisaal/gocq/internal/queue"
)

type ConcurrentVoidPriorityQueue[T any] struct {
	*ConcurrentPriorityQueue[T, any]
}

// Creates a new ConcurrentVoidPriorityQueue with the specified concurrency and worker function.
func NewVoidPriorityQueue[T any](concurrency uint32, worker VoidWorker[T]) *ConcurrentVoidPriorityQueue[T] {
	queue := &ConcurrentQueue[T, any]{
		concurrency:   concurrency,
		worker:        worker,
		channelsStack: make([]chan *job.Job[T, any], concurrency),
		jobQueue:      queue.NewPriorityQueue[*job.Job[T, any]](),
	}

	return &ConcurrentVoidPriorityQueue[T]{
		ConcurrentPriorityQueue: &ConcurrentPriorityQueue[T, any]{
			ConcurrentQueue: queue.Init(),
		},
	}
}

// Add adds a new Job with the given priority to the queue.
func (q *ConcurrentVoidPriorityQueue[T]) Add(data T, priority int) job.AwaitableVoidJob[T] {
	j := &job.Job[T, any]{
		Data: data,
		ResultChannel: &job.ResultChannel[any]{
			Err: make(chan error, 1),
		},
	}

	q.addJob(j, queue.EnqItem[*job.Job[T, any]]{Value: j, Priority: priority})

	return j
}

// AddAll adds multiple Jobs with the given items to the queue and returns a channel to receive all error responses.
func (q *ConcurrentVoidPriorityQueue[T]) AddAll(items []PQItem[T]) <-chan error {
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

		q.addJob(j, queue.EnqItem[*job.Job[T, any]]{Value: j, Priority: item.Priority})
	}

	go func() {
		wg.Wait()

		channel.Close()
		close(response)
	}()

	return response
}

// Pause pauses the processing of jobs.
func (q *ConcurrentVoidPriorityQueue[T]) Pause() *ConcurrentVoidPriorityQueue[T] {
	q.pause()
	return q
}
