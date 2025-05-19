package varmq

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goptics/varmq/internal/collections"
	"github.com/goptics/varmq/utils"
)

// WorkerFunc represents a function that processes a Job and returns a result and an error.
type WorkerFunc[T, R any] func(T) (R, error)

// WorkerErrFunc represents a function that processes a Job and returns an error.
type WorkerErrFunc[T any] func(T) error

// VoidWorkerFunc represents a function that processes a Job and returns nothing.
type VoidWorkerFunc[T any] func(T)

type status = uint32

const (
	initiated status = iota
	running
	paused
	stopped
)

var (
	errWorkerAlreadyBound = errors.New("the worker is already bound to a queue")
	errInvalidWorkerType  = errors.New("invalid worker type passed to worker")
	errRunningWorker      = errors.New("worker is already running")
	errNotRunningWorker   = errors.New("worker is not running")
)

type worker[T, R any] struct {
	workerFunc      any
	concurrency     atomic.Uint32
	channelStack    *collections.Stack[chan iJob[T, R]]
	CurProcessing   atomic.Uint32
	Queue           IBaseQueue
	Cache           ICache
	status          atomic.Uint32
	jobPullNotifier utils.Notifier
	wg              sync.WaitGroup
	tickers         *sync.Map
	configs
}

// Worker represents a worker that processes Jobs.
type Worker[T, R any] interface {
	// IsPaused returns whether the worker is paused.
	IsPaused() bool
	// IsStopped returns whether the worker is stopped.
	IsStopped() bool
	// IsRunning returns whether the worker is running.
	IsRunning() bool
	// Status returns the current status of the worker.
	Status() string
	// CurrentProcessingCount returns the number of Jobs currently being processed by the worker.
	CurrentProcessingCount() int
	// CurrentConcurrency returns the current concurrency or pool size of the worker.
	CurrentConcurrency() int
	// Copy returns a copy of the worker.
	Copy(config ...any) IWorkerBinder[T, R]
	// TuneConcurrency tunes (increase or decrease) the pool size of the worker.
	TuneConcurrency(concurrency int) error
	// Pause pauses the worker.
	Pause() Worker[T, R]
	// PauseAndWait pauses the worker and waits until all ongoing processes are done.
	PauseAndWait()
	// Stop stops the worker and waits until all ongoing processes are done to gracefully close the channels.
	// Time complexity: O(n) where n is the number of channels
	Stop()
	// Restart restarts the worker and initializes new worker goroutines based on the concurrency.
	// Time complexity: O(n) where n is the concurrency
	Restart() error
	// Resume continues processing jobs those are pending in the queue.
	// Time complexity: O(n) where n is the concurrency
	Resume() error
}

// newWorker creates a new worker with the given worker function and configurations
// The worker function can be any of WorkerFunc, WorkerErrFunc, or VoidWorkerFunc
// It initializes the worker with the configured concurrency, cache, and other settings
func newWorker[T, R any](wf any, configs ...any) *worker[T, R] {
	c := loadConfigs(configs...)

	w := &worker[T, R]{
		workerFunc:      wf,
		concurrency:     atomic.Uint32{},
		channelStack:    collections.NewStack[chan iJob[T, R]](c.Concurrency),
		Queue:           getNullQueue(),
		Cache:           c.Cache,
		jobPullNotifier: utils.NewNotifier(1),
		configs:         c,
		wg:              sync.WaitGroup{},
		tickers:         new(sync.Map),
	}

	w.concurrency.Store(c.Concurrency)

	return w
}

func (w *worker[T, R]) setQueue(q IBaseQueue) {
	w.Queue = q
}

func (w *worker[T, R]) setCache(c ICache) {
	w.Cache = c
}

func (w *worker[T, R]) isNullCache() bool {
	return w.Cache == getCache()
}

// spawnWorker starts a worker goroutine to process jobs from the specified channel
// It continuously reads jobs from the channel and processes each one
// Each job processing is wrapped in its own function with proper cleanup
// Time complexity: O(1) per job
func (w *worker[T, R]) spawnWorker(channel chan iJob[T, R]) {
	for j := range channel {
		func() {
			defer w.wg.Done()
			defer w.jobPullNotifier.Send()
			defer w.CurProcessing.Add(^uint32(0)) // Decrement the processing counter
			defer w.freeChannel(channel)          // push back the free channel to the stack to be used for the next job
			defer j.close()
			defer j.ChangeStatus(finished)

			w.processSingleJob(j)
		}()
	}
}

// processSingleJob processes a single job using the appropriate worker function type
// It handles all three worker function types (VoidWorkerFunc, WorkerErrFunc, WorkerFunc)
// and safely captures any panics that might occur during processing
// It also sends any errors or results back to the job's result channel
func (w *worker[T, R]) processSingleJob(j iJob[T, R]) {
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
		err = errInvalidWorkerType
	}

	// send error if any
	if err := selectError(panicErr, err); err != nil {
		j.SaveAndSendError(err)
	}
}

// startEventLoop starts the event loop that processes pending jobs when workers become available
// It continuously checks if the worker is running, has available capacity, and if there are jobs in the queue
// When all conditions are met, it processes the next job in the queue
// Time complexity: O(1) per notification
func (w *worker[T, R]) startEventLoop() {
	w.jobPullNotifier.Receive(func() {
		for w.IsRunning() && w.CurProcessing.Load() < w.concurrency.Load() && w.Queue.Len() > 0 {
			w.processNextJob()
		}
	})
}

// processNextJob processes the next Job in the queue.
func (w *worker[T, R]) processNextJob() {
	var v any
	var ok bool
	var ackId string

	switch q := w.Queue.(type) {
	case IAcknowledgeable:
		v, ok, ackId = q.DequeueWithAckId()
	default:
		v, ok = q.Dequeue()
	}

	if !ok {
		return
	}

	w.wg.Add(1)
	var j iJob[T, R]

	// check the type of the value
	// and cast it to the appropriate job type
	switch value := v.(type) {
	case iJob[T, R]:
		j = value
	case []byte:
		var err error
		if j, err = parseToJob[T, R](value); err != nil {
			return
		}

		if cachedJob, ok := w.Cache.Load(j.ID()); ok {
			j = cachedJob.(iJob[T, R])
		} else {
			w.Cache.Store(j.ID(), j)
			j.SetInternalQueue(w.Queue)
		}
	default:
		return
	}

	if j.IsClosed() {
		w.wg.Done()
		w.Cache.Delete(j.ID())
		// process next Job recursively if the current one is closed
		w.processNextJob()
		return
	}

	w.CurProcessing.Add(1)
	j.ChangeStatus(processing)
	j.SetAckId(ackId)

	// then job will be process by the processSingleJob function inside spawnWorker
	w.pickNextChannel() <- j
}

func (w *worker[T, R]) freeChannel(channel chan iJob[T, R]) {
	// if the queue length is greater than or equal to the concurrency
	// then push the channel to the stack
	if w.Queue.Len() >= w.CurrentConcurrency() {
		w.channelStack.Push(channel)
		return
	}

	// else close the channel to remove idle workers
	close(channel)
}

// pickNextChannel picks the next available channel for processing a Job.
// Time complexity: O(1)
func (w *worker[T, R]) pickNextChannel() chan<- iJob[T, R] {
	// pop the last free channel
	if channel, ok := w.channelStack.Pop(); ok {
		return channel
	}

	// if the channel stack is empty, create a new channel and spawn a worker
	channel := make(chan iJob[T, R], 1)
	go w.spawnWorker(channel)

	return channel
}

// notifyToPullNextJobs notifies the pullNextJobs function to process the next Job.
func (w *worker[T, R]) notifyToPullNextJobs() {
	w.jobPullNotifier.Send()
}

// cleanupCacheInterval starts a background process that periodically cleans up finished jobs from the cache
// It checks if a ticker is already running for this cache, and if so, returns without creating another one
// If the interval is > 0, it will create a ticker that triggers cleanup at the specified interval
func (w *worker[T, R]) cleanupCacheInterval(interval time.Duration) {
	_, ok := w.tickers.Load(w.Cache)

	// if the ticker is already running for the same cache, return
	if ok {
		return
	}

	ticker := time.NewTicker(interval)
	w.tickers.Store(w.Cache, ticker)

	for range ticker.C {
		w.Cache.Range(func(key, value any) bool {
			if j, ok := value.(iJob[T, R]); ok && j.Status() == "Closed" {
				w.Cache.Delete(key)
			}
			return true
		})
	}
}

func (w *worker[T, R]) stopTickers() {
	defer w.tickers.Clear()

	w.tickers.Range(func(key, value any) bool {
		if ticker, ok := value.(*time.Ticker); ok {
			ticker.Stop()
		}

		return true
	})
}

func (w *worker[T, R]) waitUnitCurrentProcessing() {
	for w.CurrentProcessingCount() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
}

func (w *worker[T, R]) start() error {
	if w.IsRunning() {
		return errRunningWorker
	}

	// if cache is been set and cleanup interval is not set, use default cleanup interval for 10 minutes
	if !w.isNullCache() && w.configs.CleanupCacheInterval == 0 {
		w.configs.CleanupCacheInterval = 10 * time.Minute
	}

	defer w.notifyToPullNextJobs()
	defer w.status.Store(running)

	go w.startEventLoop()

	if w.configs.CleanupCacheInterval > 0 {
		go w.cleanupCacheInterval(w.configs.CleanupCacheInterval)
	}

	return nil
}

func (w *worker[T, R]) TuneConcurrency(concurrency int) error {
	if w.status.Load() != running {
		return errNotRunningWorker
	}

	w.concurrency.Store(withSafeConcurrency(concurrency))

	return nil
}

func (w *worker[T, R]) Copy(config ...any) IWorkerBinder[T, R] {
	c := mergeConfigs(w.configs, config...)

	newWorker := &worker[T, R]{
		workerFunc:      w.workerFunc,
		concurrency:     atomic.Uint32{},
		Queue:           getNullQueue(),
		Cache:           c.Cache,
		channelStack:    collections.NewStack[chan iJob[T, R]](c.Concurrency),
		jobPullNotifier: utils.NewNotifier(1),
		wg:              sync.WaitGroup{},
		configs:         c,
		tickers:         w.tickers,
	}

	newWorker.concurrency.Store(c.Concurrency)

	return newQueues(newWorker)
}

func (w *worker[T, R]) CurrentConcurrency() int {
	return int(w.concurrency.Load())
}

func (w *worker[T, R]) Pause() Worker[T, R] {
	w.status.Store(paused)
	return w
}

func (w *worker[T, R]) Stop() {
	defer w.status.Store(stopped)
	w.stopTickers()

	// wait until all ongoing processes are done to gracefully close the channels
	w.jobPullNotifier.Close()
	w.PauseAndWait()

	// Close all channels in the stack
	w.channelStack.Range(func(channel chan iJob[T, R]) {
		close(channel)
	})

	w.channelStack.Clear()
	w.Cache.Clear()
}

func (w *worker[T, R]) Restart() error {
	// first pause the queue to avoid routine leaks or deadlocks
	// wait until all ongoing processes are done to gracefully close the channels if any.
	w.PauseAndWait()
	// close the old notifier to avoid routine leaks
	w.jobPullNotifier.Close()
	w.jobPullNotifier = utils.NewNotifier(1)

	if err := w.start(); err != nil {
		return err
	}

	// resume the queue to process pending Jobs
	w.Resume()
	return nil
}

func (w *worker[T, R]) IsPaused() bool {
	return w.status.Load() == paused
}

func (w *worker[T, R]) IsRunning() bool {
	return w.status.Load() == running
}

func (w *worker[T, R]) IsStopped() bool {
	return w.status.Load() == stopped
}

func (w *worker[T, R]) Status() string {
	switch w.status.Load() {
	case initiated:
		return "Initiated"
	case running:
		return "Running"
	case paused:
		return "Paused"
	case stopped:
		return "Stopped"
	default:
		return "Unknown"
	}
}

func (w *worker[T, R]) CurrentProcessingCount() int {
	return int(w.CurProcessing.Load())
}

func (w *worker[T, R]) Resume() error {
	if w.status.Load() == initiated {
		return w.start()
	}

	if w.IsRunning() {
		return errRunningWorker
	}

	w.status.Store(running)
	w.jobPullNotifier.Send()

	return nil
}

func (w *worker[T, R]) PauseAndWait() {
	w.Pause()
	w.waitUnitCurrentProcessing()
}
