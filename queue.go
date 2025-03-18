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
	ChannelsStack []chan job.Job[T, R]
	curProcessing atomic.Uint32
	JobQueue      queue.IQueue[job.Job[T, R]]
	wg            sync.WaitGroup
	mx            sync.Mutex
	isPaused      atomic.Bool
	jobNotifier   job.Notifier
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
		ChannelsStack: make([]chan job.Job[T, R], concurrency),
		JobQueue:      queue.NewQueue[job.Job[T, R]](),
	}

	concurrentQueue.Restart()
	return concurrentQueue
}

func (q *concurrentQueue[T, R]) Restart() {
	// first pause the queue to avoid routine leaks or deadlocks
	q.Pause()
	// wait until all ongoing processes are done to gracefully close the channels if any.
	q.WaitUntilFinished()

	if q.jobNotifier != nil {
		q.jobNotifier.Close()
	}

	q.jobNotifier = job.NewNotifier()

	// restart the queue with new channels and start the worker goroutines
	for i := range q.ChannelsStack {
		// close old channels to avoid routine leaks
		if q.ChannelsStack[i] != nil {
			close(q.ChannelsStack[i])
		}

		// This channel stack is used to pick the next available channel for processing a Job inside a worker goroutine.
		q.ChannelsStack[i] = make(chan job.Job[T, R], 1)
		go q.spawnWorker(q.ChannelsStack[i])
	}
	go q.processJobs()

	// resume the queue to process pending Jobs
	q.Resume()
}

// spawnWorker starts a worker goroutine to process jobs from the channel.
func (q *concurrentQueue[T, R]) spawnWorker(channel chan job.Job[T, R]) {
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
		j.Close()
		q.freeChannel(channel)
	}
}

func (q *concurrentQueue[T, R]) freeChannel(channel chan job.Job[T, R]) {
	q.mx.Lock()
	defer q.mx.Unlock()
	// push the channel back to the stack, so it can be used for the next Job
	q.ChannelsStack = append(q.ChannelsStack, channel)
	q.curProcessing.Add(^uint32(0))
	q.wg.Done()
	q.jobNotifier.Notify()
}

// processJobs processes the jobs in the queue every time a new Job is added.
func (q *concurrentQueue[T, R]) processJobs() {
	q.jobNotifier.Listen(func() {
		for !q.isPaused.Load() && q.curProcessing.Load() < q.Concurrency && q.JobQueue.Len() > 0 {
			q.processNextJob()
		}
	})
}

// pickNextChannel picks the next available channel for processing a Job.
// Time complexity: O(1)
func (q *concurrentQueue[T, R]) pickNextChannel() chan<- job.Job[T, R] {
	q.mx.Lock()
	defer q.mx.Unlock()
	l := len(q.ChannelsStack)

	// pop the last free channel
	channel := q.ChannelsStack[l-1]
	q.ChannelsStack = q.ChannelsStack[:l-1]
	return channel
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

	q.curProcessing.Add(1)
	j.ChangeStatus(job.Processing)

	q.pickNextChannel() <- j
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
	return q.curProcessing.Load()
}

func (q *concurrentQueue[T, R]) Pause() ConcurrentQueue[T, R] {
	q.PauseQueue()
	return q
}

func (q *concurrentQueue[T, R]) AddJob(enqItem queue.EnqItem[job.Job[T, R]]) {
	q.wg.Add(1)
	q.mx.Lock()
	defer q.mx.Unlock()
	q.JobQueue.Enqueue(enqItem)
	enqItem.Value.ChangeStatus(job.Queued)

	q.jobNotifier.Notify()
}

func (q *concurrentQueue[T, R]) Resume() error {
	if !q.isPaused.Load() {
		return errors.New("queue is already running")
	}

	q.isPaused.Store(false)
	q.jobNotifier.Notify()

	return nil
}

func (q *concurrentQueue[T, R]) Add(data T) types.EnqueuedJob[R] {
	j := job.New[T, R](data)

	q.AddJob(queue.EnqItem[job.Job[T, R]]{Value: j})
	return j
}

func (q *concurrentQueue[T, R]) AddAll(data []T) types.EnqueuedGroupJob[R] {
	groupJob := job.NewGroupJob[T, R](uint32(len(data)))

	for _, item := range data {
		q.AddJob(queue.EnqItem[job.Job[T, R]]{Value: groupJob.NewJob(item)})
	}

	return groupJob
}

func (q *concurrentQueue[T, R]) WaitUntilFinished() {
	q.wg.Wait()
}

func (q *concurrentQueue[T, R]) Purge() {
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
	q.jobNotifier.Close()
	for _, channel := range q.ChannelsStack {
		if channel == nil {
			continue
		}

		close(channel)
	}

	return nil
}

func (q *concurrentQueue[T, R]) WaitAndClose() error {
	q.wg.Wait()
	return q.Close()
}
