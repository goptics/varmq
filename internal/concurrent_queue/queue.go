package concurrent_queue

import (
	"sync"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/internal/job"
	"github.com/fahimfaisaal/gocq/internal/queue"
)

type ConcurrentQueue[T, R any] struct {
	Concurrency   uint32
	Worker        any
	ChannelsStack []chan *job.Job[T, R]
	curProcessing uint32
	JobQueue      queue.IQueue[*job.Job[T, R]]
	wg            sync.WaitGroup
	mx            sync.Mutex
	isPaused      atomic.Bool
}

type IConcurrentQueue[T, R any] interface {
	IQueue[T, R]
	Pause() IConcurrentQueue[T, R]
	Add(data T) EnqueuedJob[R]
	AddAll(data []T) <-chan Result[R]
}

// Creates a new ConcurrentQueue with the specified concurrency and worker function.
// Internally it calls Init() to start the worker goroutines based on the concurrency.
func NewQueue[T, R any](concurrency uint32, worker Worker[T, R]) *ConcurrentQueue[T, R] {
	queue := &ConcurrentQueue[T, R]{
		Concurrency:   concurrency,
		Worker:        worker,
		ChannelsStack: make([]chan *job.Job[T, R], concurrency),
		JobQueue:      queue.NewQueue[*job.Job[T, R]](),
	}

	queue.Restart()
	return queue
}

// Restart restarts the queue and initializes the worker goroutines based on the concurrency.
// Time complexity: O(n) where n is the concurrency
func (q *ConcurrentQueue[T, R]) Restart() {
	q.isPaused.Store(false)

	for i := range q.ChannelsStack {
		// close old channels to avoid routine leaks
		if q.ChannelsStack[i] != nil {
			close(q.ChannelsStack[i])
		}

		q.ChannelsStack[i] = make(chan *job.Job[T, R])
		go q.spawnWorker(q.ChannelsStack[i])
	}
}

// spawnWorker starts a worker goroutine to process jobs from the channel.
func (q *ConcurrentQueue[T, R]) spawnWorker(channel chan *job.Job[T, R]) {
	for j := range channel {
		switch worker := q.Worker.(type) {
		case VoidWorker[T]:
			err := worker(j.Data)
			j.ResultChannel.Err <- err
		case Worker[T, R]:
			result, err := worker(j.Data)
			if err != nil {
				j.ResultChannel.Err <- err
			} else {
				j.ResultChannel.Data <- result
			}
		default:
			// do nothing
		}

		j.ChangeStatus(job.Finished)
		j.Close()
		q.wg.Done()

		q.mx.Lock()
		q.ChannelsStack = append(q.ChannelsStack, channel)
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
	l := len(q.ChannelsStack)

	// pop the last free channel
	channel := q.ChannelsStack[l-1]
	q.ChannelsStack = q.ChannelsStack[:l-1]
	return channel
}

// shouldProcessNextJob determines if the next job should be processed based on the current state.
func (q *ConcurrentQueue[T, R]) shouldProcessNextJob(action string) bool {
	switch action {
	case "add":
		return !q.isPaused.Load() && q.curProcessing < q.Concurrency
	case "resume":
		return q.curProcessing < q.Concurrency && q.JobQueue.Len() > 0
	case "worker":
		return !q.isPaused.Load() && q.JobQueue.Len() != 0
	default:
		return false
	}
}

// PauseQueue pauses the processing of jobs.
func (q *ConcurrentQueue[T, R]) PauseQueue() {
	q.isPaused.Store(true)
}

// processNextJob processes the next Job in the queue.
func (q *ConcurrentQueue[T, R]) processNextJob() {
	j, has := q.JobQueue.Dequeue()

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
		q.pickNextChannel() <- job
	}(j)
}

// PendingCount returns the number of Jobs pending in the queue.
// Time complexity: O(1)
func (q *ConcurrentQueue[T, R]) PendingCount() int {
	return q.JobQueue.Len()
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
func (q *ConcurrentQueue[T, R]) Pause() IConcurrentQueue[T, R] {
	q.PauseQueue()
	return q
}

func (q *ConcurrentQueue[T, R]) AddJob(job *job.Job[T, R], enqItem queue.EnqItem[*job.Job[T, R]]) {
	q.wg.Add(1)
	q.mx.Lock()
	defer q.mx.Unlock()
	q.JobQueue.Enqueue(enqItem)

	// process next Job only when the current processing Job count is less than the concurrency
	if q.shouldProcessNextJob("add") {
		q.processNextJob()
	}
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
func (q *ConcurrentQueue[T, R]) Add(data T) EnqueuedJob[R] {
	j := &job.Job[T, R]{
		Data: data,
		ResultChannel: &job.ResultChannel[R]{
			Data: make(chan R, 1),
			Err:  make(chan error, 1),
		},
	}

	q.AddJob(j, queue.EnqItem[*job.Job[T, R]]{Value: j})
	return j
}

// AddAll adds multiple Jobs to the queue and returns a channel to receive all responses.
// Time complexity: O(n) where n is the number of Jobs added
func (q *ConcurrentQueue[T, R]) AddAll(data []T) <-chan Result[R] {
	wg := sync.WaitGroup{}
	response := make(chan Result[R], len(data))
	dataCh, err := make(chan R, q.Concurrency), make(chan error, q.Concurrency)
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
					response <- Result[R]{Data: val}
					wg.Done()
				} else {
					return
				}
			case err, ok := <-err:
				if ok {
					response <- Result[R]{Err: err}
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

		q.AddJob(j, queue.EnqItem[*job.Job[T, R]]{Value: j})
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

	prevValues := q.JobQueue.Values()
	q.JobQueue.Init()
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

	// wait until all ongoing processes are done to gracefully close the channels
	q.wg.Wait()

	for _, channel := range q.ChannelsStack {
		if channel == nil {
			continue
		}

		close(channel)
	}

	q.ChannelsStack = make([]chan *job.Job[T, R], q.Concurrency)
	return nil
}

// WaitAndClose waits until all pending Jobs in the queue are processed and then closes the queue.
// Time complexity: O(n) where n is the number of pending Jobs
func (q *ConcurrentQueue[T, R]) WaitAndClose() error {
	q.wg.Wait()
	return q.Close()
}
