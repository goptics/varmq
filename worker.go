package gocq

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
	"github.com/fahimfaisaal/gocq/v2/shared/utils"
)

type WorkerFunc[T, R any] func(T) (R, error)
type WorkerErrFunc[T any] func(T) error
type VoidWorkerFunc[T any] func(T)

type worker[T, R any] struct {
	workerFunc      any
	Concurrency     uint32
	ChannelsStack   []chan job.Job[T, R]
	CurProcessing   atomic.Uint32
	Queue           types.IQueue
	Cache           Cache
	isPaused        atomic.Bool
	jobPullNotifier job.Notifier
	sync            syncGroup
	isDistributed   bool
}

type Worker[T, R any] interface {
	IsPaused() bool
	// Restart restarts the queue and initializes the worker goroutines based on the concurrency.
	// Time complexity: O(n) where n is the concurrency
	Pause() Worker[T, R]

	PauseAndWait()

	// WithCache sets the cache for the queue.
	WithCache(cache Cache) Worker[T, R]

	Start() error

	Stop()

	Restart() error
	// Resume continues processing jobs those are pending in the queue.
	Resume() error
	// CurrentProcessingCount returns the number of Jobs currently being processed.
	// Time complexity: O(1)
	CurrentProcessingCount() uint32

	notify()

	queue() types.IQueue

	cache() Cache

	syncGroup() *syncGroup
}

func newWorker[T, R any](w any, configs ...Config) *worker[T, R] {
	c := loadConfigs[T, R](configs...)

	return &worker[T, R]{
		workerFunc:      w,
		Concurrency:     c.Concurrency,
		ChannelsStack:   make([]chan job.Job[T, R], c.Concurrency),
		Queue:           c.Queue,
		Cache:           c.Cache,
		jobPullNotifier: job.NewNotifier(1),
		isDistributed:   c.IsDistributed,
		sync:            syncGroup{},
	}
}

func (w *worker[T, R]) queue() types.IQueue {
	return w.Queue
}

func (w *worker[T, R]) cache() Cache {
	return w.Cache
}

func (w *worker[T, R]) syncGroup() *syncGroup {
	return &w.sync
}

func (w *worker[T, R]) notify() {
	w.jobPullNotifier.Notify()
}


func (w *worker[T, R]) listenEnqueueNotification() {
	for range w.Queue.NotificationChannel() {
		w.sync.wg.Add(1)
		w.jobPullNotifier.Notify()
	}
}

// freeChannel frees the channel for the next Job.
func (w *worker[T, R]) freeChannel(channel chan job.Job[T, R]) {
	w.sync.mx.Lock()
	defer w.sync.mx.Unlock()
	// push the channel back to the stack, so it can be used for the next Job
	w.ChannelsStack = append(w.ChannelsStack, channel)
}

// spawnWorker starts a worker goroutine to process jobs from the channel.
func (w *worker[T, R]) spawnWorker(channel chan job.Job[T, R]) {
	for j := range channel {
		func() {
			defer w.sync.wg.Done()
			defer w.jobPullNotifier.Notify()
			defer w.CurProcessing.Add(^uint32(0))
			defer w.freeChannel(channel)
			defer j.Close()
			defer j.ChangeStatus(job.Finished)

			w.processSingleJob(j)
		}()
	}
}

func (w *worker[T, R]) processSingleJob(j job.Job[T, R]) {
	var panicErr error
	var err error

	switch worker := w.workerFunc.(type) {
	case VoidWorkerFunc[T]:
		panicErr = utils.WithSafe("void worker", func() {
			worker(j.Data())
		})

	case WorkerErrFunc[T]:
		panicErr = utils.WithSafe("worker error", func() {
			err = worker(j.Data())
		})

	case WorkerFunc[T, R]:
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
		err = errors.New("unsupported worker type passed to queue")
	}

	// send error if any
	if err := selectError(panicErr, err); err != nil {
		j.SaveAndSendError(err)
	}
}

// pullNextJobs processes the jobs in the queue every time a new Job is added.
func (w *worker[T, R]) pullNextJobs() {
	w.jobPullNotifier.Listen(func() {
		for !w.isPaused.Load() && w.CurProcessing.Load() < w.Concurrency && w.Queue.Len() > 0 {
			w.processNextJob()
		}
	})
}

// processNextJob processes the next Job in the queue.
func (w *worker[T, R]) processNextJob() {
	v, ok := w.Queue.Dequeue()
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
		cachedJob, ok := w.Cache.Load(j.ID())

		if ok {
			j = cachedJob.(job.Job[T, R])
		} else {
			w.Cache.Store(j.ID(), j)
		}
	default:
		return
	}

	if j.IsClosed() {
		w.sync.wg.Done()
		w.Cache.Delete(j.ID())

		// process next Job recursively if the current one is closed
		w.processNextJob()
		return
	}

	w.CurProcessing.Add(1)
	j.ChangeStatus(job.Processing)

	// then job will be process by the processSingleJob function inside spawnWorker
	w.pickNextChannel() <- j
}

// pickNextChannel picks the next available channel for processing a Job.
// Time complexity: O(1)
func (w *worker[T, R]) pickNextChannel() chan<- job.Job[T, R] {
	w.sync.mx.Lock()
	defer w.sync.mx.Unlock()
	l := len(w.ChannelsStack)

	// pop the last free channel
	channel := w.ChannelsStack[l-1]
	w.ChannelsStack = w.ChannelsStack[:l-1]
	return channel
}

func (w *worker[T, R]) Start() error {
	w.sync.wg.Add(w.Queue.Len())
	// restart the queue with new channels and start the worker goroutines
	for i := range w.ChannelsStack {
		// close old channels to avoid routine leaks
		if w.ChannelsStack[i] != nil {
			close(w.ChannelsStack[i])
		}

		// This channel stack is used to pick the next available channel for processing a Job inside a worker goroutine.
		w.ChannelsStack[i] = make(chan job.Job[T, R], 1)
		go w.spawnWorker(w.ChannelsStack[i])
	}

	go w.pullNextJobs()

	if w.isDistributed {
		go w.listenEnqueueNotification()
	}

	fmt.Println("worker started")

	return nil
}

func (w *worker[T, R]) Stop() {
	// wait until all ongoing processes are done to gracefully close the channels
	w.jobPullNotifier.Close()
	for _, channel := range w.ChannelsStack {
		if channel == nil {
			continue
		}

		close(channel)
	}

	w.Cache = getCache()
	w.ChannelsStack = make([]chan job.Job[T, R], w.Concurrency)
}

func (w *worker[T, R]) Restart() error {
	// first pause the queue to avoid routine leaks or deadlocks
	// wait until all ongoing processes are done to gracefully close the channels if any.
	w.PauseAndWait()

	// close the old notifier to avoid routine leaks
	w.jobPullNotifier.Close()
	w.jobPullNotifier = job.NewNotifier(1)

	err := w.Start()

	// resume the queue to process pending Jobs
	w.Resume()
	return err
}

func (w *worker[T, R]) IsPaused() bool {
	return w.isPaused.Load()
}

func (w *worker[T, R]) CurrentProcessingCount() uint32 {
	return w.CurProcessing.Load()
}

func (w *worker[T, R]) Pause() Worker[T, R] {
	w.isPaused.Store(true)
	return w
}

func (w *worker[T, R]) Resume() error {
	if !w.isPaused.Load() {
		return errors.New("queue is already running")
	}

	w.isPaused.Store(false)
	w.jobPullNotifier.Notify()

	return nil
}

func (w *worker[T, R]) WithCache(cache Cache) Worker[T, R] {
	w.Cache = cache
	return w
}

func (w *worker[T, R]) PauseAndWait() {
	w.Pause()
	w.sync.wg.Wait()
}
