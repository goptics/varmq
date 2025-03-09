package gocq

import (
	"sync"
	"sync/atomic"

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
func NewPriorityQueue[T, R any](concurrency uint, worker Worker[T, R]) *ConcurrentPriorityQueue[T, R] {
	queue := &ConcurrentQueue[T, R]{
		concurrency:   concurrency,
		worker:        worker,
		channelsStack: make([]chan *queue.Job[T, R], concurrency),
		jobQueue:      queue.NewPriorityQueue[*queue.Job[T, R]](),
		wg:            new(sync.WaitGroup),
		mx:            new(sync.Mutex),
		isPaused:      atomic.Bool{},
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
func (q *ConcurrentPriorityQueue[T, R]) Add(data T, priority int) <-chan R {
	q.mx.Lock()
	defer q.mx.Unlock()

	job := &queue.Job[T, R]{
		Data:     data,
		Response: make(chan R, 1),
	}

	q.jobQueue.Enqueue(queue.EnqItem[*queue.Job[T, R]]{Value: job, Priority: priority})
	q.wg.Add(1)

	// process next Job only when the current processing Job count is less than the concurrency
	if q.shouldProcessNextJob("add") {
		q.processNextJob()
	}

	return job.Response
}

// AddAll adds multiple Jobs with the given priority to the queue and returns a channel to receive all responses.
// Time complexity: O(n log n) where n is the number of Jobs added
func (q *ConcurrentPriorityQueue[T, R]) AddAll(items []PQItem[T]) <-chan R {
	wg := new(sync.WaitGroup)
	merged := make(chan R, len(items))

	wg.Add(len(items))
	for _, item := range items {
		go func(c <-chan R) {
			defer wg.Done()
			for val := range c {
				merged <- val
			}
		}(q.Add(item.Value, item.Priority))
	}

	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged
}
