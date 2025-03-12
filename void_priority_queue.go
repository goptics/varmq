package gocq

import (
	"sync"

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
		channelsStack: make([]chan *queue.Job[T, any], concurrency),
		jobQueue:      queue.NewPriorityQueue[*queue.Job[T, any]](),
	}

	return &ConcurrentVoidPriorityQueue[T]{
		ConcurrentPriorityQueue: &ConcurrentPriorityQueue[T, any]{
			ConcurrentQueue: queue.Init(),
		},
	}
}

// Add adds a new Job with the given priority to the queue.
func (q *ConcurrentVoidPriorityQueue[T]) Add(data T, priority int) <-chan error {
	job := &queue.Job[T, any]{
		Data: data,
		ResultChannel: &queue.ResultChannel[any]{
			Err: make(chan error, 1),
		},
	}

	q.addJob(job, queue.EnqItem[*queue.Job[T, any]]{Value: job, Priority: priority})

	return job.ResultChannel.Err
}

func (q *ConcurrentVoidPriorityQueue[T]) AddAll(items []PQItem[T]) <-chan error {
	wg := new(sync.WaitGroup)
	response := make(chan error, len(items))
	err := make(chan error, 1)
	channel := &queue.ResultChannel[any]{
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
		job := &queue.Job[T, any]{
			Data:          item.Value,
			ResultChannel: channel,
			Lock:          true,
		}

		q.addJob(job, queue.EnqItem[*queue.Job[T, any]]{Value: job, Priority: item.Priority})
	}

	go func() {
		wg.Wait()

		close(err)
		close(response)
	}()

	return response
}

// Pause pauses the processing of jobs.
func (q *ConcurrentVoidPriorityQueue[T]) Pause() *ConcurrentVoidPriorityQueue[T] {
	q.pause()
	return q
}
