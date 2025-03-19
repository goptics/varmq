package gocq

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
	"github.com/fahimfaisaal/gocq/v2/shared/utils"
)

type concurrentQueue[T, R any] struct {
	Concurrency uint32
	Worker      any
	// channels for each concurrency level and store them in a stack.
	ChannelsStack []chan job.Job[T, R]
	curProcessing atomic.Uint32
	JobQueue      queue.IQueue[job.Job[T, R]]
	jobCache      Cache
	wg            sync.WaitGroup
	mx            sync.Mutex
	isPaused      atomic.Bool
	jobPuller     job.Notifier
}

type ConcurrentQueue[T, R any] interface {
	ICQueue[R]
	// WithCache sets the cache for the queue.
	WithCache(cache Cache) ConcurrentQueue[T, R]
	// Pause pauses the processing of jobs.
	Pause() ConcurrentQueue[T, R]
	// Add adds a new Job to the queue and returns a EnqueuedJob to handle the job.
	// Time complexity: O(1)
	Add(data T, id ...string) types.EnqueuedJob[R]
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedGroupJob to handle the job.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) types.EnqueuedGroupJob[R]
}

type Item[T any] struct {
	ID    string
	Value T
}

// Creates a new concurrentQueue with the specified concurrency and worker function.
// Internally it calls Init() to start the worker goroutines based on the concurrency.
func newQueue[T, R any](concurrency uint32, worker any) *concurrentQueue[T, R] {
	concurrentQueue := &concurrentQueue[T, R]{
		Concurrency:   concurrency,
		Worker:        worker,
		ChannelsStack: make([]chan job.Job[T, R], concurrency),
		JobQueue:      queue.NewQueue[job.Job[T, R]](),
		jobCache:      getCache(),
		jobPuller:     job.NewNotifier(),
	}

	concurrentQueue.Restart()
	return concurrentQueue
}

func (q *concurrentQueue[T, R]) processSingleJob(j job.Job[T, R]) {
	var panicErr error
	var err error

	switch worker := q.Worker.(type) {
	case VoidWorker[T]:
		panicErr = utils.WithSafe("void worker", func() {
			err = worker(j.Data())
		})

	case Worker[T, R]:
		panicErr = utils.WithSafe("worker", func() {
			result, e := worker(j.Data())
			if e != nil {
				err = e
			} else {
				j.SaveAndSendResult(result)
			}
		})
	default:
		// Log or handle the invalid type to avoid silent failures
		j.SaveAndSendError(errors.New("unsupported worker type passed to queue"))
	}

	// send error if any
	if err := selectError(panicErr, err); err != nil {
		j.SaveAndSendError(err)
	}
}

// spawnWorker starts a worker goroutine to process jobs from the channel.
func (q *concurrentQueue[T, R]) spawnWorker(channel chan job.Job[T, R]) {
	for j := range channel {
		func() {
			defer q.wg.Done()
			defer q.jobPuller.Notify()
			defer q.curProcessing.Add(^uint32(0))
			defer q.freeChannel(channel)
			defer j.Close()
			defer j.ChangeStatus(job.Finished)

			q.processSingleJob(j)
		}()
	}
}

func (q *concurrentQueue[T, R]) freeChannel(channel chan job.Job[T, R]) {
	q.mx.Lock()
	defer q.mx.Unlock()
	// push the channel back to the stack, so it can be used for the next Job
	q.ChannelsStack = append(q.ChannelsStack, channel)
}

// pullNextJobs processes the jobs in the queue every time a new Job is added.
func (q *concurrentQueue[T, R]) pullNextJobs() {
	q.jobPuller.Listen(func() {
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
	v, ok := q.JobQueue.Dequeue()
	if !ok {
		return
	}

	var j job.Job[T, R]

	switch value := v.(type) {
	case job.Job[T, R]:
		j = value
	case []byte:
		var err error
		j, err = job.ParseToJob[T, R](value)
		if err != nil {
			return
		}
	default:
		return
	}

	if j.IsClosed() {
		q.wg.Done()
		q.jobCache.Delete(j.ID())

		// process next Job recursively if the current one is closed
		q.processNextJob()
		return
	}

	q.curProcessing.Add(1)
	j.ChangeStatus(job.Processing)

	q.pickNextChannel() <- j
}

func (q *concurrentQueue[T, R]) Restart() {
	// first pause the queue to avoid routine leaks or deadlocks
	q.Pause()
	// wait until all ongoing processes are done to gracefully close the channels if any.
	q.WaitUntilFinished()

	// close the old notifier to avoid routine leaks
	if q.jobPuller != nil {
		q.jobPuller.Close()
	}

	q.jobPuller = job.NewNotifier()

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

	go q.pullNextJobs()

	// resume the queue to process pending Jobs
	q.Resume()
}

func (q *concurrentQueue[T, R]) WithCache(cache Cache) ConcurrentQueue[T, R] {
	q.jobCache = cache
	return q
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

func (q *concurrentQueue[T, R]) postEnqueue(j job.Job[T, R]) {
	j.ChangeStatus(job.Queued)
	q.jobPuller.Notify()

	if id := j.ID(); id != "" {
		q.jobCache.Store(id, j)
	}
}

func (q *concurrentQueue[T, R]) JobById(id string) (types.EnqueuedJob[R], error) {
	val, ok := q.jobCache.Load(id)
	if !ok {
		return nil, fmt.Errorf("job not found for id: %s", id)
	}

	return val.(types.EnqueuedJob[R]), nil
}

func (q *concurrentQueue[T, R]) GroupsJobById(id string) (types.EnqueuedSingleGroupJob[R], error) {
	val, ok := q.jobCache.Load(job.GenerateGroupId(id))

	if !ok {
		return nil, fmt.Errorf("group job not found for id: %s", id)
	}

	return val.(types.EnqueuedSingleGroupJob[R]), nil
}

func (q *concurrentQueue[T, R]) Resume() error {
	if !q.isPaused.Load() {
		return errors.New("queue is already running")
	}

	q.isPaused.Store(false)
	q.jobPuller.Notify()

	return nil
}

func (q *concurrentQueue[T, R]) Add(data T, id ...string) types.EnqueuedJob[R] {
	j := job.New[T, R](data, id...)

	q.wg.Add(1)
	q.JobQueue.Enqueue(queue.EnqItem[job.Job[T, R]]{Value: j})
	q.postEnqueue(j)

	return j
}

func (q *concurrentQueue[T, R]) AddAll(items []Item[T]) types.EnqueuedGroupJob[R] {
	groupJob := job.NewGroupJob[T, R](uint32(len(items)))

	q.wg.Add(len(items))
	for _, item := range items {
		j := groupJob.NewJob(item.Value, item.ID)
		q.JobQueue.Enqueue(queue.EnqItem[job.Job[T, R]]{Value: j})
		q.postEnqueue(j)
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
	q.jobPuller.Close()
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
