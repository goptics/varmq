package gocq

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/types"
)

type concurrentQueue[T, R any] struct {
	Concurrency uint32
	Worker      any
	// channels for each concurrency level and store them in a stack.
	ChannelsStack []chan job.IJob[T, R]
	curProcessing uint32
	JobQueue      queue.IQueue[job.IJob[T, R]]
	wg            sync.WaitGroup
	mx            sync.Mutex
	isPaused      atomic.Bool
}

type ConcurrentQueue[T, R any] interface {
	ICQueue
	// Pause pauses the processing of jobs.
	Pause() ConcurrentQueue[T, R]
	// Add adds a new Job to the queue and returns a EnqueuedJob to handle the job.
	// Time complexity: O(1)
	Add(data T) types.EnqueuedJob[R]
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedGroupJob to handle the job.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []T) types.EnqueuedGroupJob[R]
}

// Creates a new concurrentQueue with the specified concurrency and worker function.
// Internally it calls Init() to start the worker goroutines based on the concurrency.
func newQueue[T, R any](concurrency uint32, worker any) *concurrentQueue[T, R] {
	concurrentQueue := &concurrentQueue[T, R]{
		Concurrency:   concurrency,
		Worker:        worker,
		ChannelsStack: make([]chan job.IJob[T, R], concurrency),
		JobQueue:      queue.NewQueue[job.IJob[T, R]](),
	}

	concurrentQueue.Restart()
	return concurrentQueue
}

func (q *concurrentQueue[T, R]) Restart() {
	// first pause the queue to avoid routine leaks or deadlocks
	q.Pause()
	// wait until all ongoing processes are done to gracefully close the channels if any.
	q.WaitUntilFinished()

	// restart the queue with new channels and start the worker goroutines
	for i := range q.ChannelsStack {
		// close old channels to avoid routine leaks
		if q.ChannelsStack[i] != nil {
			close(q.ChannelsStack[i])
		}

		// This channel stack is used to pick the next available channel for processing a Job inside a worker goroutine.
		q.ChannelsStack[i] = make(chan job.IJob[T, R])
		go q.spawnWorker(q.ChannelsStack[i])
	}

	// resume the queue to process pending Jobs
	q.Resume()
}

// spawnWorker starts a worker goroutine to process jobs from the channel.
func (q *concurrentQueue[T, R]) spawnWorker(channel chan job.IJob[T, R]) {
	for j := range channel {
		switch worker := q.Worker.(type) {
		case VoidWorker[T]:
			err := worker(j.Data())
			j.SendError(err)
		case Worker[T, R]:
			result, err := worker(j.Data())
			if err != nil {
				j.SendError(err)
			} else {
				j.SendResult(result)
			}
		default:
			// Log or handle the invalid type to avoid silent failures
			j.SendError(errors.New("unsupported worker type passed to queue"))
		}

		j.ChangeStatus(job.Finished)

		switch jobType := j.(type) {
		case job.IGroupJob[T, R]:
			jobType.Done()
		case job.IJob[T, R]:
			jobType.Close()
		}

		q.mx.Lock()
		// push the channel back to the stack, so it can be used for the next Job
		q.ChannelsStack = append(q.ChannelsStack, channel)
		q.curProcessing--
		q.wg.Done()

		if q.shouldProcessNextJob("worker") {
			q.processNextJob()
		}
		q.mx.Unlock()
	}
}

// pickNextChannel picks the next available channel for processing a Job.
// Time complexity: O(1)
func (q *concurrentQueue[T, R]) pickNextChannel() chan<- job.IJob[T, R] {
	q.mx.Lock()
	defer q.mx.Unlock()
	l := len(q.ChannelsStack)

	// pop the last free channel
	channel := q.ChannelsStack[l-1]
	q.ChannelsStack = q.ChannelsStack[:l-1]
	return channel
}

// shouldProcessNextJob determines if the next job should be processed based on the current state.
func (q *concurrentQueue[T, R]) shouldProcessNextJob(action string) bool {
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

// processNextJob processes the next Job in the queue.
func (q *concurrentQueue[T, R]) processNextJob() {
	j, has := q.JobQueue.Dequeue()

	if !has {
		return
	}

	if j.IsClosed() {
		q.wg.Done()
		// process next Job recursively if the current one is closed
		q.processNextJob()
		return
	}

	q.curProcessing++
	j.ChangeStatus(job.Processing)

	go func(job job.IJob[T, R]) {
		q.pickNextChannel() <- job
	}(j)
}

// PauseQueue pauses the processing of jobs.
func (q *concurrentQueue[T, R]) PauseQueue() {
	q.isPaused.Store(true)
}

func (q *concurrentQueue[T, R]) PendingCount() int {
	return q.JobQueue.Len()
}

func (q *concurrentQueue[T, R]) IsPaused() bool {
	return q.isPaused.Load()
}

func (q *concurrentQueue[T, R]) CurrentProcessingCount() uint32 {
	return q.curProcessing
}

func (q *concurrentQueue[T, R]) Pause() ConcurrentQueue[T, R] {
	q.PauseQueue()
	return q
}

func (q *concurrentQueue[T, R]) AddJob(enqItem queue.EnqItem[job.IJob[T, R]]) {
	q.wg.Add(1)
	q.mx.Lock()
	defer q.mx.Unlock()
	q.JobQueue.Enqueue(enqItem)
	enqItem.Value.ChangeStatus(job.Queued)

	// process next Job only when the current processing Job count is less than the concurrency
	if q.shouldProcessNextJob("add") {
		q.processNextJob()
	}
}

func (q *concurrentQueue[T, R]) Resume() {
	q.isPaused.Store(false)

	// Process pending jobs if any
	q.mx.Lock()
	defer q.mx.Unlock()

	// Process jobs up to concurrency limit
	for q.shouldProcessNextJob("resume") {
		q.processNextJob()
	}
}

func (q *concurrentQueue[T, R]) Add(data T) types.EnqueuedJob[R] {
	j := job.New[T, R](data)

	q.AddJob(queue.EnqItem[job.IJob[T, R]]{Value: j})
	return j
}

func (q *concurrentQueue[T, R]) AddAll(data []T) types.EnqueuedGroupJob[R] {
	groupJob := job.NewGroupJob[T, R](uint32(len(data)))

	for _, item := range data {
		q.AddJob(queue.EnqItem[job.IJob[T, R]]{Value: groupJob.NewJob(item)})
	}

	return groupJob
}

func (q *concurrentQueue[T, R]) WaitUntilFinished() {
	q.wg.Wait()
}

func (q *concurrentQueue[T, R]) Purge() {
	q.mx.Lock()
	defer q.mx.Unlock()

	prevValues := q.JobQueue.Values()
	q.JobQueue.Init()
	q.wg.Add(-len(prevValues))

	// close all pending channels to avoid routine leaks
	for _, job := range prevValues {
		job.CloseResultChannel()
	}
}

func (q *concurrentQueue[T, R]) Close() error {
	q.Purge()

	// wait until all ongoing processes are done to gracefully close the channels
	q.wg.Wait()

	for _, channel := range q.ChannelsStack {
		if channel == nil {
			continue
		}

		close(channel)
	}

	q.ChannelsStack = make([]chan job.IJob[T, R], q.Concurrency)
	return nil
}

func (q *concurrentQueue[T, R]) WaitAndClose() error {
	q.wg.Wait()
	return q.Close()
}
