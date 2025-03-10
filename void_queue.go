package gocq

import (
	"sync"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/internal/queue"
)

type VoidWorker[T any] func(T) error

type ConcurrentVoidQueue[T any] struct {
	*ConcurrentQueue[T, any]
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and worker function.
func NewVoidQueue[T any](concurrency uint, worker VoidWorker[T]) *ConcurrentVoidQueue[T] {
	queue := &ConcurrentQueue[T, any]{
		concurrency:   concurrency,
		worker:        worker,
		channelsStack: make([]chan *queue.Job[T, any], concurrency),
		jobQueue:      queue.NewPriorityQueue[*queue.Job[T, any]](),
		wg:            new(sync.WaitGroup),
		mx:            new(sync.Mutex),
		isPaused:      atomic.Bool{},
	}

	return &ConcurrentVoidQueue[T]{
		ConcurrentQueue: queue.Init(),
	}
}

// Add adds a new Job to the queue.
func (q *ConcurrentVoidQueue[T]) Add(data T) <-chan error {
	q.mx.Lock()
	defer q.mx.Unlock()

	job := &queue.Job[T, any]{
		Data: data,
		Err:  make(chan error, 1),
	}

	q.jobQueue.Enqueue(queue.EnqItem[*queue.Job[T, any]]{Value: job})
	q.wg.Add(1)

	// process next Job only when the current processing Job count is less than the concurrency
	if q.shouldProcessNextJob("add") {
		q.processNextJob()
	}

	return job.Err
}

// AddAll adds multiple Jobs to the queue.
func (q *ConcurrentVoidQueue[T]) AddAll(data []T) <-chan error {
	wg := new(sync.WaitGroup)
	mergedErr := make(chan error, 1)

	wg.Add(len(data))
	for _, item := range data {
		go func(err <-chan error) {
			defer wg.Done()
			for e := range err {
				mergedErr <- e
			}
		}(q.Add(item))
	}

	go func() {
		wg.Wait()
		close(mergedErr)
	}()

	return mergedErr
}

// Pause pauses the processing of jobs.
func (q *ConcurrentVoidQueue[T]) Pause() *ConcurrentVoidQueue[T] {
	q.pause()
	return q
}
