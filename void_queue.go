package gocq

import (
	"sync"

	"github.com/fahimfaisaal/gocq/internal/job"
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
		channelsStack: make([]chan *job.Job[T, any], concurrency),
		jobQueue:      queue.NewPriorityQueue[*job.Job[T, any]](),
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
func (q *ConcurrentVoidQueue[T]) Add(data T) job.AwaitableVoidJob[T] {
	j := &job.Job[T, any]{
		Data: data,
		ResultChannel: &job.ResultChannel[any]{
			Err: make(chan error, 1),
		},
	}

	q.addJob(j, queue.EnqItem[*job.Job[T, any]]{Value: j})

	return j
}

func (q *ConcurrentVoidQueue[T]) AddAll(data []T) <-chan error {
	wg := new(sync.WaitGroup)
	response := make(chan error, len(data))
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

	wg.Add(len(data))
	for _, item := range data {
		j := &job.Job[T, any]{
			Data:          item,
			ResultChannel: channel,
			Lock:          true,
		}

		q.addJob(j, queue.EnqItem[*job.Job[T, any]]{Value: j})
	}

	go func() {
		wg.Wait()

		channel.Close()
		close(response)
	}()

	return response
}
