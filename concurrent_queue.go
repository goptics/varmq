package gocq

import (
	"sync"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/internal/job"
	"github.com/fahimfaisaal/gocq/internal/queue"
)

type Response[T any] struct {
	Data T
	Err  error
}

type Worker[T, R any] func(T) (R, error)

type ConcurrentQueue[T, R any] struct {
	concurrency   uint32
	worker        any
	channelsStack []chan *job.Job[T, R]
	curProcessing uint32
	jobQueue      queue.IQueue[*job.Job[T, R]]
	wg            sync.WaitGroup
	mx            sync.Mutex
	isPaused      atomic.Bool
}

// Creates a new ConcurrentQueue with the specified concurrency and worker function.
// Internally it calls Init() to start the worker goroutines based on the concurrency.
func NewQueue[T, R any](concurrency uint32, worker Worker[T, R]) *ConcurrentQueue[T, R] {
	queue := &ConcurrentQueue[T, R]{
		concurrency:   concurrency,
		worker:        worker,
		channelsStack: make([]chan *job.Job[T, R], concurrency),
		jobQueue:      queue.NewQueue[*job.Job[T, R]](),
	}

	return queue.Init()
}

// Initializes the ConcurrentQueue by starting the worker goroutines.
// Time complexity: O(n) where n is the concurrency
func (q *ConcurrentQueue[T, R]) Init() *ConcurrentQueue[T, R] {
	for i := range q.channelsStack {
		q.channelsStack[i] = make(chan *job.Job[T, R])
		go q.spawnWorker(q.channelsStack[i])
	}
	return q
}

// spawnWorker starts a worker goroutine to process jobs from the channel.
func (q *ConcurrentQueue[T, R]) spawnWorker(channel chan *job.Job[T, R]) {
	for j := range channel {
		switch worker := q.worker.(type) {
		case VoidWorker[T]:
			err := worker(j.Data)
			j.ResultChannel.Err <- err
		case Worker[T, R]:
			output, err := worker(j.Data)
			if err != nil {
				j.ResultChannel.Err <- err
			} else {
				j.ResultChannel.Data <- output
			}
		default:
			// do nothing
		}

		j.ChangeStatus(job.Finished)
		j.Close()
		q.wg.Done()

		q.mx.Lock()
		q.channelsStack = append(q.channelsStack, channel)
		q.curProcessing--

		if q.shouldProcessNextJob("worker") {
			q.processNextJob()
		}
		q.mx.Unlock()
	}
}

// pickNextChannel picks the next available channel for processing a Job.
// Time complexity: O(1)
func (q *ConcurrentQueue[T, R]) pickNextChannel() chan<- *job.Job[T, R] {
	q.mx.Lock()
	defer q.mx.Unlock()
	l := len(q.channelsStack)

	// pop the last free channel
	channel := q.channelsStack[l-1]
	q.channelsStack = q.channelsStack[:l-1]
	return channel
}

// shouldProcessNextJob determines if the next job should be processed based on the current state.
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

// pause pauses the processing of jobs.
func (q *ConcurrentQueue[T, R]) pause() {
	q.isPaused.Store(true)
}

// pause pauses the processing of jobs.
func (q *ConcurrentQueue[T, R]) addJob(job *job.Job[T, R], enqItem queue.EnqItem[*job.Job[T, R]]) {
	q.wg.Add(1)
	q.mx.Lock()
	defer q.mx.Unlock()
	q.jobQueue.Enqueue(enqItem)

	// process next Job only when the current processing Job count is less than the concurrency
	if q.shouldProcessNextJob("add") {
		q.processNextJob()
	}
}

// processNextJob processes the next Job in the queue.
func (q *ConcurrentQueue[T, R]) processNextJob() {
	j, has := q.jobQueue.Dequeue()

	if !has {
		return
	}

	if j.IsClosed() {
		q.wg.Done()
		return
	}

	q.curProcessing++
	j.ChangeStatus(job.Processing)

	go func(job *job.Job[T, R]) {
		q.pickNextChannel() <- j
	}(j)
}

// PendingCount returns the number of Jobs pending in the queue.
// Time complexity: O(1)
func (q *ConcurrentQueue[T, R]) PendingCount() int {
	return q.jobQueue.Len()
}

// IsPaused returns whether the queue is paused.
func (q *ConcurrentQueue[T, R]) IsPaused() bool {
	return q.isPaused.Load()
}

// CurrentProcessingCount returns the number of Jobs currently being processed.
// Time complexity: O(1)
func (q *ConcurrentQueue[T, R]) CurrentProcessingCount() uint32 {
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

// Add adds a new Job to the queue and returns a channel to receive the response.
// Time complexity: O(1)
func (q *ConcurrentQueue[T, R]) Add(data T) job.AwaitableJob[R] {
	j := &job.Job[T, R]{
		Data: data,
		ResultChannel: &job.ResultChannel[R]{
			Data: make(chan R, 1),
			Err:  make(chan error, 1),
		},
	}

	q.addJob(j, queue.EnqItem[*job.Job[T, R]]{Value: j})
	return j
}

// AddAll adds multiple Jobs to the queue and returns a channel to receive all responses.
// Time complexity: O(n) where n is the number of Jobs added
func (q *ConcurrentQueue[T, R]) AddAll(data []T) <-chan Response[R] {
	wg := sync.WaitGroup{}
	response := make(chan Response[R], len(data))
	dataCh, err := make(chan R, q.concurrency), make(chan error, q.concurrency)
	channel := &job.ResultChannel[R]{
		Data: dataCh,
		Err:  err,
	}

	// consume data and err channels from the worker
	go func() {
		for {
			select {
			case val, ok := <-dataCh:
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

	wg.Add(len(data))
	for _, item := range data {
		j := &job.Job[T, R]{
			Data:          item,
			ResultChannel: channel,
			Lock:          true,
		}

		q.addJob(j, queue.EnqItem[*job.Job[T, R]]{Value: j})
	}

	go func() {
		wg.Wait()

		channel.Close()
		close(response)
	}()

	return response
}

// WaitUntilFinished waits until all pending Jobs in the queue are processed.
// Time complexity: O(n) where n is the number of pending Jobs
func (q *ConcurrentQueue[T, R]) WaitUntilFinished() {
	q.wg.Wait()
}

// Purge removes all pending Jobs from the queue.
func (q *ConcurrentQueue[T, R]) Purge() {
	q.mx.Lock()
	defer q.mx.Unlock()

	prevValues := q.jobQueue.Values()
	q.jobQueue.Init()
	q.wg.Add(-len(prevValues))

	// close all pending channels to avoid routine leaks
	for _, job := range prevValues {
		if job.ResultChannel.Data == nil {
			continue
		}

		close(job.ResultChannel.Data)
	}
}

// Close closes the queue and resets all internal states.
// Time complexity: O(n) where n is the number of channels
func (q *ConcurrentQueue[T, R]) Close() error {
	q.Purge()

	// wait until all ongoing processes are done
	q.wg.Wait()

	for _, channel := range q.channelsStack {
		if channel == nil {
			continue
		}

		close(channel)
	}

	q.channelsStack = make([]chan *job.Job[T, R], q.concurrency)
	return nil
}

// WaitAndClose waits until all pending Jobs in the queue are processed and then closes the queue.
// Time complexity: O(n) where n is the number of pending Jobs
func (q *ConcurrentQueue[T, R]) WaitAndClose() error {
	q.wg.Wait()
	return q.Close()
}
