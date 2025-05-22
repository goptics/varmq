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
	errInvalidWorkerType = errors.New("invalid worker type passed to worker")
	errRunningWorker     = errors.New("worker is already running")
	errNotRunningWorker  = errors.New("worker is not running")
	errSameConcurrency   = errors.New("worker already has the same concurrency")
)

type worker[T, R any] struct {
	workerFunc      any
	concurrency     atomic.Uint32
	pool            *collections.List[poolNode[T, R]]
	CurProcessing   atomic.Uint32
	Queue           IBaseQueue
	Cache           ICache
	status          atomic.Uint32
	jobPullNotifier utils.Notifier
	wg              sync.WaitGroup
	tickers         []*time.Ticker
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
	// NumProcessing returns the number of Jobs currently being processed by the worker.
	NumProcessing() int
	// NumConcurrency returns the current concurrency or pool size of the worker.
	NumConcurrency() int
	// NumIdleWorkers returns the number of idle workers in the pool.
	NumIdleWorkers() int
	// Copy returns a copy of the worker.
	Copy(config ...any) IWorkerBinder[T, R]
	// TunePool tunes (increase or decrease) the pool size of the worker.
	TunePool(concurrency int) error
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
		pool:            collections.NewList[poolNode[T, R]](),
		Queue:           getNullQueue(),
		Cache:           c.Cache,
		jobPullNotifier: utils.NewNotifier(1),
		configs:         c,
		wg:              sync.WaitGroup{},
		tickers:         make([]*time.Ticker, 0),
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
func (w *worker[T, R]) spawnWorker(node *collections.Node[poolNode[T, R]]) {
	for j := range node.Value.ch {
		w.processSingleJob(j)

		j.ChangeStatus(finished)
		j.close()
		w.freePoolNode(node)            // push back the free channel to the stack to be used for the next job
		w.CurProcessing.Add(^uint32(0)) // Decrement the processing counter
		w.notifyToPullNextJobs()
		w.wg.Done()
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

func (w *worker[T, R]) freePoolNode(node *collections.Node[poolNode[T, R]]) {
	// If worker timeout is enabled, update the last used time
	enabledIdleWorkersRemover := w.configs.IdleWorkerExpiryDuration > 0

	if enabledIdleWorkersRemover {
		node.Value.UpdateLastUsed()
	}

	// If queue length is high or we're under our idle worker target, keep this worker
	if w.Queue.Len() >= w.NumConcurrency() || enabledIdleWorkersRemover || w.pool.Len() < w.numMinIdleWorkers() {
		w.pool.PushNode(node)
		return
	}

	// Otherwise close this channel to reduce idle workers
	node.Value.Close()
}

// pickNextChannel picks the next available channel for processing a Job.
// Time complexity: O(1)
func (w *worker[T, R]) pickNextChannel() chan<- iJob[T, R] {
	// pop the last free channel
	if node := w.pool.PopBack(); node != nil {
		return node.Value.ch
	}

	// if the channel stack is empty, create a new channel and spawn a worker
	return w.initPoolNode().Value.ch
}

func (w *worker[T, R]) initPoolNode() *collections.Node[poolNode[T, R]] {
	node := collections.NewNode(newPoolNode[T, R](1))
	// Start a worker goroutine to process jobs from this nodes channel
	go w.spawnWorker(node)
	return node
}

// notifyToPullNextJobs notifies the pullNextJobs function to process the next Job.
func (w *worker[T, R]) notifyToPullNextJobs() {
	w.jobPullNotifier.Send()
}

// goCleanupCache starts a background process that periodically cleans up finished jobs from the cache
// It checks if a ticker is already running for this cache, and if so, returns without creating another one
// If the interval is > 0, it will create a ticker that triggers cleanup at the specified interval
func (w *worker[T, R]) goCleanupCache() {
	interval := w.configs.CleanupCacheInterval

	if interval == 0 {
		return
	}

	ticker := time.NewTicker(interval)
	w.tickers = append(w.tickers, ticker)

	go func() {
		for range ticker.C {
			w.Cache.Range(func(key, value any) bool {
				if j, ok := value.(iJob[T, R]); ok && j.Status() == "Closed" {
					w.Cache.Delete(key)
				}
				return true
			})
		}
	}()
}

// numMinIdleWorkers returns the number of idle workers to keep based on concurrency and config percentage
func (w *worker[T, R]) numMinIdleWorkers() int {
	percentage := w.configs.MinIdleWorkerRatio
	concurrency := w.concurrency.Load()

	return int(max((concurrency*uint32(percentage))/100, 1))
}

func (w *worker[T, R]) goRemoveIdleWorkers() {
	interval := w.configs.IdleWorkerExpiryDuration

	if interval == 0 {
		return
	}

	ticker := time.NewTicker(interval)
	w.tickers = append(w.tickers, ticker)

	go func() {
		for range ticker.C {
			// Calculate the target number of idle workers
			targetIdleWorkers := w.numMinIdleWorkers()

			// if the number of idle workers is less than or equal to the target, continue
			if w.pool.Len() <= targetIdleWorkers {
				continue
			}

			nodes := w.pool.NodeSlice()
			// If we have more nodes than our target, close the excess ones
			for _, node := range nodes[targetIdleWorkers:] {
				if node.Value.GetLastUsed().Add(interval).Before(time.Now()) &&
					!(node.Next() == nil && node.Prev() == nil) { // if both nil, it means the node is not in the list and not idle
					node.Value.Close()
					w.pool.Remove(node)
				}
			}
		}
	}()
}

func (w *worker[T, R]) stopTickers() {
	for _, ticker := range w.tickers {
		ticker.Stop()
	}

	w.tickers = make([]*time.Ticker, 0)
}

func (w *worker[T, R]) waitUnitCurrentProcessing() {
	for w.NumProcessing() != 0 {
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

	w.goCleanupCache()
	w.goRemoveIdleWorkers()

	// init the first worker by default
	w.pool.PushNode(w.initPoolNode())

	return nil
}

func (w *worker[T, R]) TunePool(concurrency int) error {
	if w.status.Load() != running {
		return errNotRunningWorker
	}

	oldConcurrency := w.concurrency.Load()
	safeConcurrency := withSafeConcurrency(concurrency)

	if oldConcurrency == safeConcurrency {
		return errSameConcurrency
	}

	w.concurrency.Store(safeConcurrency)

	// if new concurrency is greater than the old concurrency, then notify to pull next jobs
	// cause it will be extended by the event loop when it needs
	if safeConcurrency > oldConcurrency {
		defer w.notifyToPullNextJobs()
		return nil
	}

	// if idle worker expiry duration is set, then no need to shrink the pool size
	// cause it will be removed by the idle worker remover
	if w.configs.IdleWorkerExpiryDuration != 0 {
		return nil
	}

	shrinkPoolSize, minIdleWorkers := oldConcurrency-safeConcurrency, w.numMinIdleWorkers()

	// if current concurrency is greater than the safe concurrency, shrink the pool size
	for shrinkPoolSize > 0 && w.pool.Len() != minIdleWorkers {
		// since the channel might be busy processing a job, we need to retry until we get the channels
		if node := w.pool.PopBack(); node != nil {
			node.Value.Close()
			w.pool.Remove(node)
			shrinkPoolSize--
		}
	}

	return nil
}

func (w *worker[T, R]) Copy(config ...any) IWorkerBinder[T, R] {
	c := mergeConfigs(w.configs, config...)

	newWorker := &worker[T, R]{
		workerFunc:      w.workerFunc,
		concurrency:     atomic.Uint32{},
		Queue:           getNullQueue(),
		Cache:           c.Cache,
		pool:            collections.NewList[poolNode[T, R]](),
		jobPullNotifier: utils.NewNotifier(1),
		wg:              sync.WaitGroup{},
		configs:         c,
		tickers:         make([]*time.Ticker, 0),
	}

	newWorker.concurrency.Store(c.Concurrency)

	return newQueues(newWorker)
}

func (w *worker[T, R]) NumConcurrency() int {
	return int(w.concurrency.Load())
}

func (w *worker[T, R]) NumIdleWorkers() int {
	return w.pool.Len()
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

	// remove all nodes from the list and close the channels
	for _, node := range w.pool.NodeSlice() {
		node.Value.Close()
		w.pool.Remove(node)
	}

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

func (w *worker[T, R]) NumProcessing() int {
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
