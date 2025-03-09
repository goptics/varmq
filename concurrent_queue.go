package gocq

import (
	"sync"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/internal/queue"
)

type Worker[T, R any] func(T) R

type ConcurrentQueue[T, R any] struct {
	concurrency   uint
	worker        any
	channelsStack []chan *queue.Job[T, R]
	curProcessing uint
	jobQueue      queue.IQueue[*queue.Job[T, R]]
	wg            *sync.WaitGroup
	mx            *sync.Mutex
	isPaused      atomic.Bool
}

// Creates a new ConcurrentQueue with the specified concurrency and worker function.
// Internally it calls Init() to start the worker goroutines based on the concurrency.
func NewQueue[T, R any](concurrency uint, worker Worker[T, R]) *ConcurrentQueue[T, R] {
	channelsStack := make([]chan *queue.Job[T, R], concurrency)
	wg, mx, jobQueue := new(sync.WaitGroup), new(sync.Mutex), queue.NewQueue[*queue.Job[T, R]]()

	queue := &ConcurrentQueue[T, R]{
		concurrency:   concurrency,
		worker:        worker,
		channelsStack: channelsStack,
		curProcessing: 0,
		jobQueue:      jobQueue,
		wg:            wg,
		mx:            mx,
		isPaused:      atomic.Bool{},
	}

	return queue.Init()
}

// Initializes the ConcurrentQueue by starting the worker goroutines.
// Time complexity: O(n) where n is the concurrency
func (q *ConcurrentQueue[T, R]) Init() *ConcurrentQueue[T, R] {
	for i := range q.concurrency {
		// if channel is not nil, close it
		// reason: to avoid routine leaks
		if channel := q.channelsStack[i]; channel != nil {
			close(channel)
		}

		q.channelsStack[i] = make(chan *queue.Job[T, R])

		go q.spawnWorker(q.channelsStack[i])
	}

	return q
}

func (q *ConcurrentQueue[T, R]) spawnWorker(channel chan *queue.Job[T, R]) {
	for job := range channel {
		switch worker := q.worker.(type) {
		case VoidWorker[T]:
			worker(job.Data)
		case Worker[T, R]:
			output := worker(job.Data)
			job.Response <- output // sends the output to the Job consumer
			close(job.Response)
		default:
			// do nothing
		}

		q.wg.Done()

		q.mx.Lock()
		// adding the free channel to stack
		q.channelsStack = append(q.channelsStack, channel)
		q.curProcessing--

		// process only if the queue is not empty
		if q.shouldProcessNextJob("worker") {
			q.processNextJob()
		}
		q.mx.Unlock()
	}
}

// Picks the next available channel for processing a Job.
// Time complexity: O(1)
func (q *ConcurrentQueue[T, R]) pickNextChannel() chan<- *queue.Job[T, R] {
	q.mx.Lock()
	defer q.mx.Unlock()
	l := len(q.channelsStack)

	// pop the last free channel
	channel := q.channelsStack[l-1]
	q.channelsStack = q.channelsStack[:l-1]
	return channel
}

// Determines if the next job should be processed based on the current state.
func (q *ConcurrentQueue[T, R]) shouldProcessNextJob(action string) bool {
	switch action {
	case "add":
		return !q.isPaused.Load() && q.curProcessing < q.concurrency
	case "resume":
		return q.curProcessing < q.concurrency && q.jobQueue.Len() > 0
	case "worker":
		return !q.isPaused.Load() && q.jobQueue.Len() != 0
	default:
		return false
	}
}

func (q *ConcurrentQueue[T, R]) pause() {
	q.isPaused.Store(true)
}

// Processes the next Job in the queue.
func (q *ConcurrentQueue[T, R]) processNextJob() {
	value, has := q.jobQueue.Dequeue()

	if !has {
		return
	}

	q.curProcessing++

	go func(data *queue.Job[T, R]) {
		q.pickNextChannel() <- data
	}(value)
}

// Returns the number of Jobs pending in the queue.
// Time complexity: O(1)
func (q *ConcurrentQueue[T, R]) PendingCount() int {
	return q.jobQueue.Len()
}

// IsPaused returns whether the queue is paused.
func (q *ConcurrentQueue[T, R]) IsPaused() bool {
	return q.isPaused.Load()
}

// Returns the number of Jobs currently being processed.
// Time complexity: O(1)
func (q *ConcurrentQueue[T, R]) CurrentProcessingCount() uint {
	return q.curProcessing
}

// Pause pauses the processing of jobs.
func (q *ConcurrentQueue[T, R]) Pause() *ConcurrentQueue[T, R] {
	q.pause()
	return q
}

// Resume continues processing jobs.
func (q *ConcurrentQueue[T, R]) Resume() {
	q.isPaused.Store(false)

	// Process pending jobs if any
	q.mx.Lock()
	defer q.mx.Unlock()

	// Process jobs up to concurrency limit
	for q.shouldProcessNextJob("resume") {
		q.processNextJob()
	}
}

// Adds a new Job to the queue and returns a channel to receive the response and a cancel function.
// Time complexity: O(1)
func (q *ConcurrentQueue[T, R]) Add(data T) <-chan R {
	q.mx.Lock()
	defer q.mx.Unlock()

	job := &queue.Job[T, R]{
		Data:     data,
		Response: make(chan R, 1),
	}

	q.jobQueue.Enqueue(queue.EnqItem[*queue.Job[T, R]]{Value: job})
	q.wg.Add(1)

	// process next Job only when the current processing Job count is less than the concurrency
	if q.shouldProcessNextJob("add") {
		q.processNextJob()
	}

	return job.Response
}

// Adds multiple Jobs to the queue and returns a channel to receive all responses.
// Time complexity: O(n) where n is the number of Jobs added
func (q *ConcurrentQueue[T, R]) AddAll(data []T) <-chan R {
	wg := new(sync.WaitGroup)
	merged := make(chan R, len(data))

	wg.Add(len(data))
	for _, item := range data {
		go func(c <-chan R) {
			defer wg.Done()
			for val := range c {
				merged <- val
			}
		}(q.Add(item))
	}

	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged
}

// Waits until all pending Jobs in the queue are processed.
// Time complexity: O(n) where n is the number of pending Jobs
func (q *ConcurrentQueue[T, R]) WaitUntilFinished() {
	q.wg.Wait()
}

// Remove all pending Jobs from the queue.
func (q *ConcurrentQueue[T, R]) Purge() {
	q.mx.Lock()
	defer q.mx.Unlock()

	prevValues := q.jobQueue.Values()
	q.jobQueue.Init()
	q.wg.Add(-len(prevValues))

	// close all pending channels to avoid routine leaks
	for _, job := range prevValues {
		if job.Response == nil {
			continue
		}

		close(job.Response)
	}
}

// Closes the queue and resets all internal states.
// Time complexity: O(n) where n is the number of channels
func (q *ConcurrentQueue[T, R]) Close() {
	q.Purge()

	// wait until all ongoing processes are done
	q.wg.Wait()

	for _, channel := range q.channelsStack {
		if channel == nil {
			continue
		}

		close(channel)
	}

	q.channelsStack = make([]chan *queue.Job[T, R], q.concurrency)
}

// Waits until all pending Jobs in the queue are processed and then closes the queue.
// Time complexity: O(n) where n is the number of pending Jobs
func (q *ConcurrentQueue[T, R]) WaitAndClose() {
	q.wg.Wait()
	q.Close()
}
