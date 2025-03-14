package concurrent_queue

import (
	"sync"

	"github.com/fahimfaisaal/gocq/internal/job"
	"github.com/fahimfaisaal/gocq/internal/queue"
)

type ConcurrentPriorityQueue[T, R any] struct {
	*ConcurrentQueue[T, R]
}

type IConcurrentPriorityQueue[T, R any] interface {
	ICQueue[T, R]
	Pause() IConcurrentPriorityQueue[T, R]
	Add(data T, priority int) EnqueuedJob[R]
	AddAll(items []PQItem[T]) <-chan Result[R]
}

// NewPriorityQueue creates a new ConcurrentPriorityQueue with the specified concurrency and worker function.
func NewPriorityQueue[T, R any](concurrency uint32, worker Worker[T, R]) *ConcurrentPriorityQueue[T, R] {
	concurrentQueue := &ConcurrentQueue[T, R]{
		Concurrency:   concurrency,
		Worker:        worker,
		ChannelsStack: make([]chan *job.Job[T, R], concurrency),
		JobQueue:      queue.NewPriorityQueue[*job.Job[T, R]](),
	}

	concurrentQueue.Restart()
	return &ConcurrentPriorityQueue[T, R]{ConcurrentQueue: concurrentQueue}
}

// Pause pauses the processing of jobs.
func (q *ConcurrentPriorityQueue[T, R]) Pause() IConcurrentPriorityQueue[T, R] {
	q.PauseQueue()
	return q
}

// Add adds a new Job with the given priority to the queue and returns a channel to receive the result.
// Time complexity: O(log n)
func (q *ConcurrentPriorityQueue[T, R]) Add(data T, priority int) EnqueuedJob[R] {
	j := job.New[T, R](data)

	q.AddJob(queue.EnqItem[*job.Job[T, R]]{Value: j, Priority: priority})

	return j
}

// AddAll adds multiple Jobs with the given priority to the queue and returns a channel to receive all responses.
// Time complexity: O(n log n) where n is the number of Jobs added
func (q *ConcurrentPriorityQueue[T, R]) AddAll(items []PQItem[T]) <-chan Result[R] {
	wg := sync.WaitGroup{}
	result := make(chan Result[R], len(items))
	channel := job.NewResultChannel[R](int(q.Concurrency))
	j := job.NewWithResultChannel[T, R](channel).Lock()

	// consume data and err channels from the worker
	go func(c *job.ResultChannel[R]) {
		for {
			select {
			case val, ok := <-c.Data:
				if ok {
					result <- Result[R]{Data: val}
					wg.Done()
				} else {
					return
				}
			case err, ok := <-c.Err:
				if ok {
					result <- Result[R]{Err: err}
					wg.Done()
				} else {
					return
				}
			}
		}
	}(channel)

	wg.Add(len(items))
	for _, item := range items {
		initJob := j.Init(item.Value)

		q.AddJob(queue.EnqItem[*job.Job[T, R]]{Value: initJob, Priority: item.Priority})
	}

	go func() {
		wg.Wait()

		channel.Close()
		close(result)
	}()

	return result
}
