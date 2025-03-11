package gocq

import (
	"sync"

	"github.com/fahimfaisaal/gocq/internal/queue"
)

type PQItem[T any] struct {
	Value    T
	Priority int
}

type ConcurrentPriorityQueue[T, R any] struct {
	*ConcurrentQueue[T, R]
}

// NewPriorityQueue creates a new ConcurrentPriorityQueue with the specified concurrency and worker function.
func NewPriorityQueue[T, R any](concurrency uint32, worker Worker[T, R]) *ConcurrentPriorityQueue[T, R] {
	queue := &ConcurrentQueue[T, R]{
		concurrency:   concurrency,
		worker:        worker,
		channelsStack: make([]chan queue.Job[T, R], concurrency),
		jobQueue:      queue.NewPriorityQueue[queue.Job[T, R]](),
	}

	return &ConcurrentPriorityQueue[T, R]{ConcurrentQueue: queue.Init()}
}

// Pause pauses the processing of jobs.
func (q *ConcurrentPriorityQueue[T, R]) Pause() *ConcurrentPriorityQueue[T, R] {
	q.pause()
	return q
}

// Add adds a new Job with the given priority to the queue and returns a channel to receive the response.
// Time complexity: O(log n)
func (q *ConcurrentPriorityQueue[T, R]) Add(data T, priority int) Awaitable[R] {
	job := queue.Job[T, R]{
		Data: data,
		Channel: queue.Channel[R]{
			Data: make(chan R, 1),
			Err:  make(chan error, 1),
		},
	}

	q.addJob(job, queue.EnqItem[queue.Job[T, R]]{Value: job, Priority: priority})

	return &job
}

// AddAll adds multiple Jobs with the given priority to the queue and returns a channel to receive all responses.
// Time complexity: O(n log n) where n is the number of Jobs added
func (q *ConcurrentPriorityQueue[T, R]) AddAll(items []PQItem[T]) <-chan Response[R] {
	wg := new(sync.WaitGroup)
	response := make(chan Response[R], len(items))
	data, err := make(chan R, q.concurrency), make(chan error, q.concurrency)

	wg.Add(len(items))
	for _, item := range items {
		job := queue.Job[T, R]{
			Data: item.Value,
			Channel: queue.Channel[R]{
				Data: data,
				Err:  err,
			},
			Lock: true,
		}

		q.addJob(job, queue.EnqItem[queue.Job[T, R]]{Value: job, Priority: item.Priority})
	}

	go func() {
		for {
			select {
			case val, ok := <-data:
				if ok {
					response <- Response[R]{Data: val}
					wg.Done()
				} else {
					return
				}
			case err, ok := <-err:
				if ok {
					response <- Response[R]{Err: err}
					wg.Done()
				} else {
					return
				}
			}
		}
	}()

	go func() {
		wg.Wait()

		close(err)
		close(data)
		close(response)
	}()

	return response
}
