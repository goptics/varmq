package gocq

import (
	"sync"

	"github.com/fahimfaisaal/gocq/internal/job"
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
		channelsStack: make([]chan *job.Job[T, R], concurrency),
		jobQueue:      queue.NewPriorityQueue[*job.Job[T, R]](),
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
func (q *ConcurrentPriorityQueue[T, R]) Add(data T, priority int) job.AwaitableJob[R] {
	j := &job.Job[T, R]{
		Data: data,
		ResultChannel: &job.ResultChannel[R]{
			Data: make(chan R, 1),
			Err:  make(chan error, 1),
		},
	}

	q.addJob(j, queue.EnqItem[*job.Job[T, R]]{Value: j, Priority: priority})

	return j
}

// AddAll adds multiple Jobs with the given priority to the queue and returns a channel to receive all responses.
// Time complexity: O(n log n) where n is the number of Jobs added
func (q *ConcurrentPriorityQueue[T, R]) AddAll(items []PQItem[T]) <-chan Response[R] {
	wg := sync.WaitGroup{}
	response := make(chan Response[R], len(items))
	data, err := make(chan R, q.concurrency), make(chan error, q.concurrency)
	channel := &job.ResultChannel[R]{
		Data: data,
		Err:  err,
	}

	// consume data and err channels from the worker
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

	wg.Add(len(items))
	for _, item := range items {
		j := &job.Job[T, R]{
			Data:          item.Value,
			ResultChannel: channel,
			Lock:          true,
		}

		q.addJob(j, queue.EnqItem[*job.Job[T, R]]{Value: j, Priority: item.Priority})
	}

	go func() {
		wg.Wait()

		channel.Close()
		close(response)
	}()

	return response
}
