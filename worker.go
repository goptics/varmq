package gocq

import (
	"errors"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
	"github.com/fahimfaisaal/gocq/v2/shared/utils"
)

type WorkerFunc[T, R any] func(T) (R, error)
type WorkerErrFunc[T any] func(T) error
type VoidWorkerFunc[T any] func(T)

type Status = uint32

const (
	Initiated Status = iota
	Running
	Paused
	Stopped
)

type worker[T, R any] struct {
	workerFunc      any
	Concurrency     uint32
	ChannelsStack   []chan job.Job[T, R]
	CurProcessing   atomic.Uint32
	Queue           types.IQueue
	Cache           Cache
	status          atomic.Uint32
	jobPullNotifier job.Notifier
	sync            syncGroup
}

type Worker[T, R any] interface {
	// IsPaused returns whether the worker is paused.
	IsPaused() bool
	// IsStopped returns whether the worker is stopped.
	IsStopped() bool
	// IsRunning returns whether the worker is running.
	IsRunning() bool
	// Pause pauses the worker.
	// Time complexity: O(n) where n is the concurrency
	Pause() Worker[T, R]
	// PauseAndWait pauses the worker and waits until all ongoing processes are done.
	PauseAndWait()
	// Stop stops the worker and waits until all ongoing processes are done to gracefully close the channels.
	Stop()
	// Status returns the current status of the worker.
	Status() string
	// Restart restarts the worker and initializes new worker goroutines based on the concurrency.
	Restart() error
	// Resume continues processing jobs those are pending in the queue.
	Resume() error
	// CurrentProcessingCount returns the number of Jobs currently being processed by the worker.
	// Time complexity: O(1)
	CurrentProcessingCount() uint32
}

func newWorker[T, R any](w any, configs ...Config) *worker[T, R] {
	c := loadConfigs(configs...)

	return &worker[T, R]{
		workerFunc:      w,
		Concurrency:     c.Concurrency,
		ChannelsStack:   make([]chan job.Job[T, R], c.Concurrency),
		Queue:           queue.GetNullQueue(),
		Cache:           c.Cache,
		jobPullNotifier: job.NewNotifier(1),
		sync:            syncGroup{},
	}
}

func (w *worker[T, R]) setQueue(q types.IQueue) {
	w.Queue = q
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
		panicErr = utils.WithSafe("error worker", func() {
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
		for w.IsRunning() && w.CurProcessing.Load() < w.Concurrency && w.Queue.Len() > 0 {
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

func (w *worker[T, R]) notify() {
	w.jobPullNotifier.Notify()
}

func (w *worker[T, R]) start() error {
	defer w.notify()
	defer w.status.Store(Running)
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

	return nil
}

func (w *worker[T, R]) Pause() Worker[T, R] {
	w.status.Store(Paused)
	return w
}

func (w *worker[T, R]) Stop() {
	defer w.status.Store(Stopped)
	// wait until all ongoing processes are done to gracefully close the channels
	w.jobPullNotifier.Close()
	w.sync.wg.Wait()
	for _, channel := range w.ChannelsStack {
		if channel == nil {
			continue
		}

		close(channel)
	}

	w.Cache.Clear()
	w.ChannelsStack = make([]chan job.Job[T, R], w.Concurrency)
}

func (w *worker[T, R]) Restart() error {
	defer w.status.Store(Running)
	// first pause the queue to avoid routine leaks or deadlocks
	// wait until all ongoing processes are done to gracefully close the channels if any.
	w.PauseAndWait()

	// close the old notifier to avoid routine leaks
	w.jobPullNotifier.Close()
	w.jobPullNotifier = job.NewNotifier(1)

	err := w.start()

	// resume the queue to process pending Jobs
	w.Resume()
	return err
}

func (w *worker[T, R]) IsPaused() bool {
	return w.status.Load() == Paused
}

func (w *worker[T, R]) IsRunning() bool {
	return w.status.Load() == Running
}

func (w *worker[T, R]) IsStopped() bool {
	return w.status.Load() == Stopped
}

func (w *worker[T, R]) Status() string {
	switch w.status.Load() {
	case Initiated:
		return "Initiated"
	case Running:
		return "Running"
	case Paused:
		return "Paused"
	case Stopped:
		return "Stopped"
	default:
		return "Unknown"
	}
}

func (w *worker[T, R]) CurrentProcessingCount() uint32 {
	return w.CurProcessing.Load()
}

func (w *worker[T, R]) Resume() error {
	if w.status.Load() == Initiated {
		return w.start()
	}

	if w.IsRunning() {
		return errors.New("queue is already running")
	}

	w.status.Store(Running)
	w.jobPullNotifier.Notify()

	return nil
}

func (w *worker[T, R]) PauseAndWait() {
	w.Pause()
	w.sync.wg.Wait()
}
