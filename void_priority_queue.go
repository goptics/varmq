package gocq

import (
	"sync"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/internal/queue"
)

type ConcurrentVoidPriorityQueue[T any] struct {
	*ConcurrentPriorityQueue[T, any]
}

// Creates a new ConcurrentVoidPriorityQueue with the specified concurrency and worker function.
func NewVoidPriorityQueue[T any](concurrency uint, worker VoidWorker[T]) *ConcurrentVoidPriorityQueue[T] {
	queue := &ConcurrentQueue[T, any]{
		concurrency:   concurrency,
		worker:        worker,
		channelsStack: make([]chan *queue.Job[T, any], concurrency),
		jobQueue:      queue.NewPriorityQueue[*queue.Job[T, any]](),
		wg:            new(sync.WaitGroup),
		mx:            new(sync.Mutex),
		isPaused:      atomic.Bool{},
	}

	return &ConcurrentVoidPriorityQueue[T]{
		ConcurrentPriorityQueue: &ConcurrentPriorityQueue[T, any]{
			ConcurrentQueue: queue.Init(),
		},
	}
}

// Add adds a new Job with the given priority to the queue.
func (q *ConcurrentVoidPriorityQueue[T]) Add(data T, priority int) <-chan error {
	q.mx.Lock()
	defer q.mx.Unlock()

	job := &queue.Job[T, any]{
		Data: data,
		Err:  make(chan error, 1),
	}

	q.jobQueue.Enqueue(queue.EnqItem[*queue.Job[T, any]]{Value: job, Priority: priority})
	q.wg.Add(1)

	// process next Job only when the current processing Job count is less than the concurrency
	if q.shouldProcessNextJob("add") {
		q.processNextJob()
	}

	return job.Err
}

// AddAll adds multiple Jobs with the given priority to the queue.
func (q *ConcurrentVoidPriorityQueue[T]) AddAll(items []PQItem[T]) <-chan error {
	wg := new(sync.WaitGroup)
	mergedErr := make(chan error, 1)

	wg.Add(len(items))
	for _, item := range items {
		go func(err <-chan error) {
			defer wg.Done()
			for e := range err {
				mergedErr <- e
			}
		}(q.Add(item.Value, item.Priority))
	}

	go func() {
		wg.Wait()
		close(mergedErr)
	}()

	return mergedErr
}

// Pause pauses the processing of jobs.
func (q *ConcurrentVoidPriorityQueue[T]) Pause() *ConcurrentVoidPriorityQueue[T] {
	q.pause()
	return q
}
