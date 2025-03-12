package gocq

import (
	"sync"

	"github.com/fahimfaisaal/gocq/internal/queue"
)

type VoidWorker[T any] func(T) error

type ConcurrentVoidQueue[T any] struct {
	*ConcurrentQueue[T, any]
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and worker function.
func NewVoidQueue[T any](concurrency uint32, worker VoidWorker[T]) *ConcurrentVoidQueue[T] {
	queue := &ConcurrentQueue[T, any]{
		concurrency:   concurrency,
		worker:        worker,
		channelsStack: make([]chan *queue.Job[T, any], concurrency),
		jobQueue:      queue.NewPriorityQueue[*queue.Job[T, any]](),
	}

	return &ConcurrentVoidQueue[T]{
		ConcurrentQueue: queue.Init(),
	}
}

// Pause pauses the processing of jobs.
func (q *ConcurrentVoidQueue[T]) Pause() *ConcurrentVoidQueue[T] {
	q.pause()
	return q
}

// Add adds a new Job to the queue.
func (q *ConcurrentVoidQueue[T]) Add(data T) <-chan error {
	job := &queue.Job[T, any]{
		Data: data,
		ResultChannel: &queue.ResultChannel[any]{
			Err: make(chan error, 1),
		},
	}

	q.addJob(job, queue.EnqItem[*queue.Job[T, any]]{Value: job})

	return job.ResultChannel.Err
}

func (q *ConcurrentVoidQueue[T]) AddAll(data []T) <-chan error {
	wg := new(sync.WaitGroup)
	response := make(chan error, len(data))
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

	wg.Add(len(data))
	for _, item := range data {
		job := &queue.Job[T, any]{
			Data:          item,
			ResultChannel: channel,
			Lock:          true,
		}

		q.addJob(job, queue.EnqItem[*queue.Job[T, any]]{Value: job})
	}

	go func() {
		wg.Wait()

		channel.Close()
		close(response)
	}()

	return response
}
