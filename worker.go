package varmq

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goptics/varmq/internal/collections"
	"github.com/goptics/varmq/internal/pool"
	"github.com/goptics/varmq/utils"
)

// WorkerResultFunc represents a function that processes a Job and returns a result and an error.
type WorkerResultFunc[T, R any] func(T) (R, error)

// WorkerErrFunc represents a function that processes a Job and returns an error.
type WorkerErrFunc[T any] func(T) error

// WorkerFunc represents a function that processes a Job and returns nothing.
type WorkerFunc[T any] func(T)

type status = uint32

const (
	initiated status = iota
	running
	paused
	stopped
)

var (
	errRunningWorker    = errors.New("worker is already running")
	errNotRunningWorker = errors.New("worker is not running")
	errSameConcurrency  = errors.New("worker already has the same concurrency")
)

type worker[T any, JobType iJob[T]] struct {
	workerFunc      func(j JobType)
	concurrency     atomic.Uint32
	pool            *collections.List[pool.Node[JobType]]
	CurProcessing   atomic.Uint32
	Queue           IBaseQueue
	status          atomic.Uint32
	jobPullNotifier utils.Notifier
	wg              sync.WaitGroup
	tickers         []*time.Ticker
	Configs         configs
}

// Worker represents a worker that processes Jobs.
type Worker interface {
	queue() IBaseQueue
	configs() configs
	notifyToPullNextJobs()
	wait()

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
	// TunePool tunes (increase or decrease) the pool size of the worker.
	TunePool(concurrency int) error
	// Pause pauses the worker.
	Pause()
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
// The worker function can be any of WorkerResultFunc, WorkerErrFunc, or WorkerFunc
// It initializes the worker with the configured concurrency, cache, and other settings
func newWorker[T any](wf WorkerFunc[T], configs ...any) *worker[T, iJob[T]] {
	c := loadConfigs(configs...)

	w := &worker[T, iJob[T]]{
		workerFunc: func(j iJob[T]) {
			utils.WithSafe("worker", func() {
				wf(j.Payload())
			})
		},
		pool:            collections.NewList[pool.Node[iJob[T]]](),
		concurrency:     atomic.Uint32{},
		Queue:           getNullQueue(),
		jobPullNotifier: utils.NewNotifier(1),
		Configs:         c,
		wg:              sync.WaitGroup{},
		tickers:         make([]*time.Ticker, 0),
	}

	w.concurrency.Store(c.Concurrency)

	return w
}

func newResultWorker[T, R any](wf WorkerResultFunc[T, R], configs ...any) *worker[T, iResultJob[T, R]] {
	c := loadConfigs(configs...)

	w := &worker[T, iResultJob[T, R]]{
		workerFunc: func(j iResultJob[T, R]) {
			var panicErr error
			var err error

			panicErr = utils.WithSafe("worker", func() {
				result, e := wf(j.Payload())
				if e != nil {
					err = e
				} else {
					j.saveAndSendResult(result)
				}
			})

			// send error if any
			if err := selectError(panicErr, err); err != nil {
				j.saveAndSendError(err)
			}
		},
		pool:            collections.NewList[pool.Node[iResultJob[T, R]]](),
		concurrency:     atomic.Uint32{},
		Queue:           getNullQueue(),
		jobPullNotifier: utils.NewNotifier(1),
		Configs:         c,
		wg:              sync.WaitGroup{},
		tickers:         make([]*time.Ticker, 0),
	}

	w.concurrency.Store(c.Concurrency)

	return w
}

func (w *worker[T, JobType]) setQueue(q IBaseQueue) {
	w.Queue = q
}

func (w *worker[T, JobType]) configs() configs {
	return w.Configs
}

func (w *worker[T, JobType]) queue() IBaseQueue {
	return w.Queue
}

func (w *worker[T, JobType]) wait() {
	w.wg.Wait()
}

// spawnWorker starts a worker goroutine to process jobs from the specified channel
// It continuously reads jobs from the channel and processes each one
// Each job processing is wrapped in its own function with proper cleanup
// Time complexity: O(1) per job
func (w *worker[T, JobType]) spawnWorker(node *collections.Node[pool.Node[JobType]]) {
	for j := range node.Value.Read() {
		w.workerFunc(j)

		j.changeStatus(finished)
		j.close()
		w.freePoolNode(node)            // push back the free channel to the stack to be used for the next job
		w.CurProcessing.Add(^uint32(0)) // Decrement the processing counter
		w.notifyToPullNextJobs()
		w.wg.Done()
	}
}

// startEventLoop starts the event loop that processes pending jobs when workers become available
// It continuously checks if the worker is running, has available capacity, and if there are jobs in the queue
// When all conditions are met, it processes the next job in the queue
func (w *worker[T, JobType]) startEventLoop() {
	w.jobPullNotifier.Receive(func() {
		for w.IsRunning() && w.CurProcessing.Load() < w.concurrency.Load() && w.Queue.Len() > 0 {
			w.processNextJob()
		}
	})
}

// processNextJob processes the next Job in the queue.
func (w *worker[T, JobType]) processNextJob() {
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
	var j JobType

	// check the type of the value
	// and cast it to the appropriate job type
	switch value := v.(type) {
	case JobType:
		j = value
	case []byte:
		var err error
		if v, err = parseToJob[T](value); err != nil {
			return
		}

		j = v.(JobType)

		j.setInternalQueue(w.Queue)
	default:
		return
	}

	if j.IsClosed() {
		w.wg.Done()
		// process next Job recursively if the current one is closed
		w.processNextJob()
		return
	}

	w.CurProcessing.Add(1)
	j.changeStatus(processing)
	j.setAckId(ackId)

	// then job will be process by the processSingleJob function inside spawnWorker
	w.sendToNextChannel(j)
}

func (w *worker[T, JobType]) freePoolNode(node *collections.Node[pool.Node[JobType]]) {
	// If worker timeout is enabled, update the last used time
	enabledIdleWorkersRemover := w.Configs.IdleWorkerExpiryDuration > 0

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

// sendToNextChannel sends the job to the next available channel for processing.
// Time complexity: O(1)
func (w *worker[T, JobType]) sendToNextChannel(j JobType) {
	// pop the last free channel
	if node := w.pool.PopBack(); node != nil {
		node.Value.Send(j)
		return
	}

	// if the channel stack is empty, create a new channel and spawn a worker
	w.initPoolNode().Value.Send(j)
}

func (w *worker[T, JobType]) initPoolNode() *collections.Node[pool.Node[JobType]] {
	node := collections.NewNode(pool.NewNode[JobType](1))
	// Start a worker goroutine to process jobs from this nodes channel
	go w.spawnWorker(node)

	return node
}

// notifyToPullNextJobs notifies the pullNextJobs function to process the next Job.
func (w *worker[T, JobType]) notifyToPullNextJobs() {
	w.jobPullNotifier.Send()
}

// numMinIdleWorkers returns the number of idle workers to keep based on concurrency and config percentage
func (w *worker[T, JobType]) numMinIdleWorkers() int {
	percentage := w.Configs.MinIdleWorkerRatio
	concurrency := w.concurrency.Load()

	return int(max((concurrency*uint32(percentage))/100, 1))
}

func (w *worker[T, JobType]) goRemoveIdleWorkers() {
	interval := w.Configs.IdleWorkerExpiryDuration

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

func (w *worker[T, JobType]) stopTickers() {
	for _, ticker := range w.tickers {
		ticker.Stop()
	}

	w.tickers = make([]*time.Ticker, 0)
}

func (w *worker[T, JobType]) waitUnitCurrentProcessing() {
	for w.NumProcessing() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
}

func (w *worker[T, JobType]) start() error {
	if w.IsRunning() {
		return errRunningWorker
	}

	defer w.notifyToPullNextJobs()
	defer w.status.Store(running)

	go w.startEventLoop()

	w.goRemoveIdleWorkers()

	// init the first worker by default
	w.pool.PushNode(w.initPoolNode())

	return nil
}

func (w *worker[T, JobType]) TunePool(concurrency int) error {
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
	if w.Configs.IdleWorkerExpiryDuration != 0 {
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

func (w *worker[T, JobType]) NumConcurrency() int {
	return int(w.concurrency.Load())
}

func (w *worker[T, JobType]) NumIdleWorkers() int {
	return w.pool.Len()
}

func (w *worker[T, JobType]) Pause() {
	w.status.Store(paused)
}

func (w *worker[T, JobType]) Stop() {
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
}

func (w *worker[T, JobType]) Restart() error {
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

func (w *worker[T, JobType]) IsPaused() bool {
	return w.status.Load() == paused
}

func (w *worker[T, JobType]) IsRunning() bool {
	return w.status.Load() == running
}

func (w *worker[T, JobType]) IsStopped() bool {
	return w.status.Load() == stopped
}

func (w *worker[T, JobType]) Status() string {
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

func (w *worker[T, JobType]) NumProcessing() int {
	return int(w.CurProcessing.Load())
}

func (w *worker[T, JobType]) Resume() error {
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

func (w *worker[T, JobType]) PauseAndWait() {
	w.Pause()
	w.waitUnitCurrentProcessing()
}
