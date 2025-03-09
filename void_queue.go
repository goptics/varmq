package gocq

import (
	"sync"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/internal/queue"
)

type VoidWorker[T any] func(T)

type ConcurrentVoidQueue[T any] struct {
	*ConcurrentQueue[T, any]
}

// Creates a new ConcurrentVoidQueue with the specified concurrency and worker function.
func NewVoidQueue[T any](concurrency uint, worker VoidWorker[T]) *ConcurrentVoidQueue[T] {
	channelsStack := make([]chan *queue.Job[T, any], concurrency)
	wg, mx, jobQueue := new(sync.WaitGroup), new(sync.Mutex), queue.NewQueue[*queue.Job[T, any]]()

	queue := &ConcurrentQueue[T, any]{
		concurrency:   concurrency,
		worker:        worker,
		channelsStack: channelsStack,
		curProcessing: 0,
		jobQueue:      jobQueue,
		wg:            wg,
		mx:            mx,
		isPaused:      atomic.Bool{},
	}

	return &ConcurrentVoidQueue[T]{
		ConcurrentQueue: queue.Init(),
	}
}

func (q *ConcurrentVoidQueue[T]) Add(data T) {
	q.mx.Lock()
	defer q.mx.Unlock()

	job := &queue.Job[T, any]{
		Data: data,
	}

	q.jobQueue.Enqueue(queue.EnqItem[*queue.Job[T, any]]{Value: job})
	q.wg.Add(1)

	// process next Job only when the current processing Job count is less than the concurrency
	if q.shouldProcessNextJob("add") {
		q.processNextJob()
	}
}

func (q *ConcurrentVoidQueue[T]) AddAll(data []T) {
	for _, item := range data {
		q.Add(item)
	}
}

// Pause pauses the processing of jobs.
func (q *ConcurrentVoidQueue[T]) Pause() *ConcurrentVoidQueue[T] {
	q.pause()
	return q
}
