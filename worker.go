package varmq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goptics/varmq/internal/linkedlist"
	"github.com/goptics/varmq/internal/pool"
)

type status = uint32

const (
	initiated status = iota
	running
	paused
	stopped
)

const (
	poolChanCap        = 1
	eventLoopSignalCap = 1
	errorChanCap       = 1
)

var (
	ErrRunningWorker    = errors.New("worker is already running")
	ErrNotRunningWorker = errors.New("worker is not running")
	ErrSameConcurrency  = errors.New("worker already has the same concurrency")
	ErrFailedToDequeue  = errors.New("failed to dequeue job")
	ErrFailedToCastJob  = errors.New("failed to cast job")
	ErrGetNextQueue     = errors.New("failed to get next queue")
	ErrParseJob         = errors.New("failed to parse job")
)

type worker[T any, JobType iJob[T]] struct {
	workerFunc      func(j JobType)
	pool            *pool.Pool[JobType]
	queues          queueManager
	metrics         Metrics
	concurrency     atomic.Uint32
	curProcessing   atomic.Uint32
	status          atomic.Uint32
	eventLoopSignal chan struct{}
	errorChan       chan error
	waiters         *sync.Cond
	tickers         []*time.Ticker
	mx              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	Configs         configs
}

// Worker represents a worker that processes Jobs.
type Worker interface {
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
	// NumPending returns the number of tasks waiting to be processed.
	NumPending() int
	// NumConcurrency returns the current concurrency or pool size of the worker.
	NumConcurrency() int
	// NumIdleWorkers returns the number of idle workers in the pool.
	NumIdleWorkers() int
	// Errs returns a read-only channel that receives all errors from the worker.
	Errs() <-chan error
	// Context returns the context of the worker.
	Context() context.Context
	// Metrics returns the metrics for the worker.
	Metrics() Metrics
	// TunePool tunes (increase or decrease) the pool size of the worker.
	TunePool(concurrency int) error
	// Pause pauses the worker.
	Pause() error
	// PauseAndWait pauses the worker and waits until all ongoing processes are done.
	PauseAndWait() error
	// Stop stops the worker and waits until all ongoing processes are done to gracefully close the channels.
	Stop() error
	// Restart restarts the worker and initializes new worker goroutines based on the concurrency.
	Restart() error
	// Resume continues processing jobs those are pending in the queue.
	Resume() error
	// WaitUntilFinished waits until all pending Jobs in the are processed.
	WaitUntilFinished()
	// WaitAndStop waits until all pending Jobs in the queue are processed and then closes the queue.
	WaitAndStop() error

	configs() configs
	notifyToPullNextJobs()
}

// newWorker creates a new worker with the given worker function and configurations
func newWorker[T any](wf func(j iJob[T]), configs ...any) *worker[T, iJob[T]] {
	c := loadConfigs(configs...)

	w := &worker[T, iJob[T]]{
		concurrency:     atomic.Uint32{},
		workerFunc:      wf,
		pool:            pool.New[iJob[T]](poolChanCap),
		queues:          createQueueManager(c.strategy),
		metrics:         newMetrics(),
		eventLoopSignal: make(chan struct{}, eventLoopSignalCap),
		errorChan:       make(chan error, errorChanCap),
		tickers:         make([]*time.Ticker, 0),
		Configs:         c,
	}

	w.waiters = sync.NewCond(&w.mx)
	w.concurrency.Store(c.concurrency)

	if c.ctx != nil {
		w.ctx, w.cancel = context.WithCancel(c.ctx)
	}

	return w
}

func newErrWorker[T any](wf func(j iErrorJob[T]), configs ...any) *worker[T, iErrorJob[T]] {
	c := loadConfigs(configs...)

	w := &worker[T, iErrorJob[T]]{
		concurrency:     atomic.Uint32{},
		workerFunc:      wf,
		pool:            pool.New[iErrorJob[T]](poolChanCap),
		queues:          createQueueManager(c.strategy),
		metrics:         newMetrics(),
		eventLoopSignal: make(chan struct{}, eventLoopSignalCap),
		errorChan:       make(chan error, errorChanCap),
		tickers:         make([]*time.Ticker, 0),
		Configs:         c,
	}

	w.waiters = sync.NewCond(&w.mx)
	w.concurrency.Store(c.concurrency)

	if c.ctx != nil {
		w.ctx, w.cancel = context.WithCancel(c.ctx)
	}

	return w
}

func newResultWorker[T, R any](wf func(j iResultJob[T, R]), configs ...any) *worker[T, iResultJob[T, R]] {
	c := loadConfigs(configs...)

	w := &worker[T, iResultJob[T, R]]{
		concurrency:     atomic.Uint32{},
		workerFunc:      wf,
		pool:            pool.New[iResultJob[T, R]](poolChanCap),
		queues:          createQueueManager(c.strategy),
		metrics:         newMetrics(),
		eventLoopSignal: make(chan struct{}, eventLoopSignalCap),
		errorChan:       make(chan error, errorChanCap),
		tickers:         make([]*time.Ticker, 0),
		Configs:         c,
	}

	w.waiters = sync.NewCond(&w.mx)
	w.concurrency.Store(c.concurrency)

	if c.ctx != nil {
		w.ctx, w.cancel = context.WithCancel(c.ctx)
	}

	return w
}

func (w *worker[T, JobType]) configs() configs {
	return w.Configs
}

func (w *worker[T, JobType]) releaseWaiters(processing uint32) {
	// Early return if there's still processing happening
	if processing != 0 {
		return
	}

	// Only release waiters if worker is paused or if running with an empty queue
	if w.IsPaused() || (w.IsRunning() && w.queues.Len() == 0) {
		// Broadcast to all waiters to signal they can continue
		w.waiters.Broadcast()
	}
}

func (w *worker[T, JobType]) sendError(err error) {
	w.mx.RLock()
	select {
	case w.errorChan <- err:
	default:
	}
	w.mx.RUnlock()
}

func (w *worker[T, JobType]) Metrics() Metrics {
	return w.metrics
}

func (w *worker[T, JobType]) WaitUntilFinished() {
	condition := func() bool {
		switch w.status.Load() {
		case running:
			return w.queues.Len() > 0 || w.curProcessing.Load() > 0
		case paused, stopped:
			return w.curProcessing.Load() > 0
		default:
			return false
		}
	}

	w.mx.Lock()
	defer w.mx.Unlock()

	for condition() {
		w.waiters.Wait()
	}
}

func (w *worker[T, JobType]) Errs() <-chan error {
	return w.errorChan
}

// processNextJob processes the next Job in the queue.
func (w *worker[T, JobType]) processNextJob() error {
	queue, err := w.queues.next()

	if err != nil {
		return fmt.Errorf("%w: %w", ErrGetNextQueue, err)
	}

	var (
		v     any
		ok    bool
		ackId string
	)

	switch q := queue.(type) {
	case IAcknowledgeable:
		v, ok, ackId = q.DequeueWithAckId()
	default:
		v, ok = q.Dequeue()
	}

	if !ok {
		return ErrFailedToDequeue
	}

	var j JobType

	// check the type of the value
	// and cast it to the appropriate job type
	switch value := v.(type) {
	case JobType:
		j = value
	case []byte:
		var err error
		if v, err = parseToJob[T](value); err != nil {
			return err
		}

		if j, ok = v.(JobType); !ok {
			return ErrFailedToCastJob
		}

		j.setInternalQueue(queue)
	default:
		return ErrFailedToCastJob
	}

	if j.IsClosed() {
		return nil
	}

	w.curProcessing.Add(1)
	j.changeStatus(processing)
	j.setAckId(ackId)

	// then job will be process by the processSingleJob function inside spawnWorker
	w.sendToNextChannel(j)

	return nil
}

func (w *worker[T, JobType]) freePoolNode(node *linkedlist.Node[pool.Node[JobType]]) {
	// If worker timeout is enabled, update the last used time
	enabledIdleWorkersRemover := w.Configs.idleWorkerExpiryDuration > 0

	if enabledIdleWorkersRemover {
		node.Value.UpdateLastUsed()
	}

	// If queue length is high or we're under our idle worker target, keep this worker
	if w.queues.Len() >= w.NumConcurrency() || enabledIdleWorkersRemover || w.pool.Len() < w.numMinIdleWorkers() {
		w.pool.PushNode(node)
		return
	}

	// Otherwise stop the worker to reduce idle workers
	node.Value.Stop()
	w.pool.Cache.Put(node)
}

// sendToNextChannel sends the job to the next available channel for processing.
// Time complexity: O(1)
func (w *worker[T, JobType]) sendToNextChannel(j JobType) {
	// pop the last free node
	if node := w.pool.PopBack(); node != nil {
		node.Value.Send(j)
		return
	}

	// if the pool is empty, create a new node and spawn a worker
	w.initPoolNode().Value.Send(j)
}

func (w *worker[T, JobType]) initPoolNode() *linkedlist.Node[pool.Node[JobType]] {
	node := w.pool.Cache.Get().(*linkedlist.Node[pool.Node[JobType]])

	// Start a worker goroutine to process jobs from this nodes channel
	go node.Value.Serve(func(j JobType) {
		w.workerFunc(j)

		j.changeStatus(finished)
		if err := j.Close(); err != nil {
			w.sendError(err)
		}
		w.freePoolNode(node)
		w.releaseWaiters(w.curProcessing.Add(^uint32(0)))
		w.metrics.incCompleted()
		w.notifyToPullNextJobs()
	})

	return node
}

// notifyToPullNextJobs notifies the pullNextJobs function to process the next Job.
func (w *worker[T, JobType]) notifyToPullNextJobs() {
	w.mx.RLock()
	select {
	case w.eventLoopSignal <- struct{}{}:
	default:
		// This default case means the eventLoopSignal buffer is full or
		// no one is listening. This is generally fine as it's a non-blocking send.
	}
	w.mx.RUnlock()
}

// numMinIdleWorkers returns the number of idle workers to keep based on concurrency and config percentage
func (w *worker[T, JobType]) numMinIdleWorkers() int {
	percentage := w.Configs.minIdleWorkerRatio
	concurrency := w.concurrency.Load()

	return int(max((concurrency*uint32(percentage))/100, 1))
}

func (w *worker[T, JobType]) goRemoveIdleWorkers() {
	interval := w.Configs.idleWorkerExpiryDuration

	if interval == 0 {
		return
	}

	ticker := time.NewTicker(interval)
	w.mx.Lock()
	w.tickers = append(w.tickers, ticker)
	w.mx.Unlock()

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
					w.pool.Remove(node)
					node.Value.Stop()
					w.pool.Cache.Put(node)
				}
			}
		}
	}()
}

func (w *worker[T, JobType]) goListenToContext() {
	if w.ctx == nil {
		return
	}

	// Capture context locally to avoid race with Restart() modifying w.ctx
	go func(c context.Context) {
		<-c.Done()

		w.Stop()
	}(w.ctx)
}

// starts the event loop that processes pending jobs when workers become available
// It continuously checks if the worker is running, has available capacity, and if there are jobs in the queue
// When all conditions are met, it processes the next job in the queue
func (w *worker[T, JobType]) goEventLoop() {
	go func(signal <-chan struct{}) {
		for range signal {
			for w.IsRunning() && w.curProcessing.Load() < w.concurrency.Load() && w.queues.Len() > 0 {
				if err := w.processNextJob(); err != nil {
					w.sendError(err)
				}
			}
		}
	}(w.eventLoopSignal)
}

func (w *worker[T, JobType]) stopTickers() {
	w.mx.Lock()
	defer w.mx.Unlock()

	for _, ticker := range w.tickers {
		ticker.Stop()
	}

	w.tickers = make([]*time.Ticker, 0)
}

func (w *worker[T, JobType]) closeChannels() {
	w.mx.Lock()
	defer w.mx.Unlock()

	if w.eventLoopSignal != nil {
		close(w.eventLoopSignal)
		w.eventLoopSignal = nil
	}
	if w.errorChan != nil {
		close(w.errorChan)
		w.errorChan = nil
	}
}

// stopAndRemoveAllWorkers removes all nodes from the list and closes the pool nodes
func (w *worker[T, JobType]) stopAndRemoveAllWorkers() {
	for _, node := range w.pool.NodeSlice() {
		w.pool.Remove(node)
		node.Value.Stop()
		w.pool.Cache.Put(node)
	}
}

func (w *worker[T, JobType]) start() error {
	if w.IsRunning() {
		return ErrRunningWorker
	}

	defer w.notifyToPullNextJobs()
	defer w.status.Store(running)

	w.goEventLoop()
	w.goRemoveIdleWorkers()
	w.goListenToContext()

	// init the first worker by default
	w.pool.PushNode(w.initPoolNode())

	return nil
}

func (w *worker[T, JobType]) TunePool(concurrency int) error {
	if w.status.Load() != running {
		return ErrNotRunningWorker
	}

	oldConcurrency := w.concurrency.Load()
	safeConcurrency := withSafeConcurrency(concurrency)

	if oldConcurrency == safeConcurrency {
		return ErrSameConcurrency
	}

	w.concurrency.Store(safeConcurrency)

	// if new concurrency is greater than the old concurrency, then notify to pull next jobs
	// cause it will be extended by the event loop when it needs
	if safeConcurrency > oldConcurrency {
		w.notifyToPullNextJobs()
		return nil
	}

	// if idle worker expiry duration is set, then no need to shrink the pool size
	// cause it will be removed by the idle worker remover
	if w.Configs.idleWorkerExpiryDuration != 0 {
		return nil
	}

	shrinkPoolSize, minIdleWorkers := oldConcurrency-safeConcurrency, w.numMinIdleWorkers()

	// if current concurrency is greater than the safe concurrency, shrink the pool size
	for shrinkPoolSize > 0 && w.pool.Len() != minIdleWorkers {
		if node := w.pool.PopBack(); node != nil {
			w.pool.Remove(node)
			node.Value.Stop()
			w.pool.Cache.Put(node)
			shrinkPoolSize--
		} else {
			break
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

func (w *worker[T, JobType]) Pause() error {
	switch s := w.status.Load(); s {
	case running:
		w.status.Store(paused)
	case paused, stopped:
		return nil
	default:
		return ErrNotRunningWorker
	}

	return nil
}

func (w *worker[T, JobType]) Stop() error {
	switch s := w.status.Load(); s {
	case stopped:
		return nil
	case running:
		w.PauseAndWait()
	case paused:
		w.WaitUntilFinished()
	default:
		return ErrNotRunningWorker
	}

	if w.cancel != nil {
		defer w.cancel()
	}
	defer w.status.Store(stopped)

	w.stopTickers()
	w.closeChannels()

	w.stopAndRemoveAllWorkers()

	return nil
}

func (w *worker[T, JobType]) NumPending() int {
	return w.queues.Len()
}

func (w *worker[T, JobType]) Restart() error {
	// If worker is running, pause and wait for ongoing processes
	switch w.status.Load() {
	case running:
		if err := w.PauseAndWait(); err != nil {
			return err
		}
		// to remove idle workers if any
		w.stopAndRemoveAllWorkers()
	case paused:
		w.WaitUntilFinished()
		// to remove idle workers if any
		w.stopAndRemoveAllWorkers()
	case stopped, initiated:
		// proceed to restart
	default:
		return ErrNotRunningWorker
	}

	w.closeChannels()

	w.mx.Lock()
	w.eventLoopSignal = make(chan struct{}, eventLoopSignalCap)
	w.errorChan = make(chan error, errorChanCap)

	if w.ctx != nil {
		w.cancel()
		w.ctx, w.cancel = context.WithCancel(w.Configs.ctx)
	}
	w.mx.Unlock()

	// Reset status to initiated to allow start() to proceed
	w.status.Store(initiated)

	if err := w.start(); err != nil {
		return err
	}

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
	return int(w.curProcessing.Load())
}

func (w *worker[T, JobType]) Resume() error {
	if w.IsStopped() {
		return ErrNotRunningWorker
	}

	if w.status.Load() == initiated {
		return w.start()
	}

	if w.IsRunning() {
		return ErrRunningWorker
	}

	w.status.Store(running)
	w.notifyToPullNextJobs()

	return nil
}

func (w *worker[T, JobType]) Context() context.Context {
	return w.ctx
}

func (w *worker[T, JobType]) PauseAndWait() error {
	if err := w.Pause(); err != nil {
		return err
	}

	w.WaitUntilFinished()
	return nil
}

func (w *worker[T, JobType]) WaitAndStop() error {
	w.WaitUntilFinished()

	return w.Stop()
}
