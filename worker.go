package varmq

import (
	"errors"
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

var (
	errRunningWorker    = errors.New("worker is already running")
	errNotRunningWorker = errors.New("worker is not running")
	errSameConcurrency  = errors.New("worker already has the same concurrency")
)

type worker[T any, JobType iJob[T]] struct {
	workerFunc      func(j JobType)
	concurrency     atomic.Uint32
	pool            *linkedlist.List[pool.Node[JobType]]
	curProcessing   atomic.Uint32
	status          atomic.Uint32
	eventLoopSignal chan struct{}
	waiters         []chan struct{}
	tickers         []*time.Ticker
	mx              sync.RWMutex
	Configs         configs
	Queue           IBaseQueue
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
	// NumConcurrency returns the current concurrency or pool size of the worker.
	NumConcurrency() int
	// NumIdleWorkers returns the number of idle workers in the pool.
	NumIdleWorkers() int
	// TunePool tunes (increase or decrease) the pool size of the worker.
	TunePool(concurrency int) error
	// Pause pauses the worker.
	Pause() error
	// PauseAndWait pauses the worker and waits until all ongoing processes are done.
	PauseAndWait() error
	// Stop stops the worker and waits until all ongoing processes are done to gracefully close the channels.
	// Time complexity: O(n) where n is the number of channels
	Stop() error
	// Restart restarts the worker and initializes new worker goroutines based on the concurrency.
	// Time complexity: O(n) where n is the concurrency
	Restart() error
	// Resume continues processing jobs those are pending in the queue.
	// Time complexity: O(n) where n is the concurrency
	Resume() error

	queue() IBaseQueue
	configs() configs
	notifyToPullNextJobs()
	wait()
}

// newWorker creates a new worker with the given worker function and configurations
func newWorker[T any](wf func(j iJob[T]), configs ...any) *worker[T, iJob[T]] {
	c := loadConfigs(configs...)

	w := &worker[T, iJob[T]]{
		workerFunc:      wf,
		pool:            linkedlist.New[pool.Node[iJob[T]]](),
		concurrency:     atomic.Uint32{},
		Queue:           getNullQueue(),
		eventLoopSignal: make(chan struct{}, 1),
		Configs:         c,
		waiters:         make([]chan struct{}, 0),
		tickers:         make([]*time.Ticker, 0),
	}

	w.concurrency.Store(c.Concurrency)

	return w
}

func newErrWorker[T any](wf func(j iErrorJob[T]), configs ...any) *worker[T, iErrorJob[T]] {
	c := loadConfigs(configs...)

	w := &worker[T, iErrorJob[T]]{
		workerFunc:      wf,
		pool:            linkedlist.New[pool.Node[iErrorJob[T]]](),
		concurrency:     atomic.Uint32{},
		Queue:           getNullQueue(),
		eventLoopSignal: make(chan struct{}, 1),
		Configs:         c,
		waiters:         make([]chan struct{}, 0),
		tickers:         make([]*time.Ticker, 0),
	}

	w.concurrency.Store(c.Concurrency)

	return w
}

func newResultWorker[T, R any](wf func(j iResultJob[T, R]), configs ...any) *worker[T, iResultJob[T, R]] {
	c := loadConfigs(configs...)

	w := &worker[T, iResultJob[T, R]]{
		workerFunc:      wf,
		pool:            linkedlist.New[pool.Node[iResultJob[T, R]]](),
		concurrency:     atomic.Uint32{},
		Queue:           getNullQueue(),
		eventLoopSignal: make(chan struct{}, 1),
		Configs:         c,
		waiters:         make([]chan struct{}, 0),
		tickers:         make([]*time.Ticker, 0),
	}

	w.concurrency.Store(c.Concurrency)

	return w
}

func (w *worker[T, JobType]) setQueue(q IBaseQueue) {
	w.mx.Lock()
	defer w.mx.Unlock()

	w.Queue = q
}

func (w *worker[T, JobType]) configs() configs {
	return w.Configs
}

func (w *worker[T, JobType]) queue() IBaseQueue {
	w.mx.RLock()
	defer w.mx.RUnlock()

	return w.Queue
}

func (w *worker[T, JobType]) wait() {
	// Check if we need to wait
	// 1. If worker is paused or stopped and no jobs are processing, no need to wait
	// 2. If worker is running but queue is empty and no jobs are processing, no need to wait
	if !w.IsRunning() && w.curProcessing.Load() == 0 {
		return
	}

	if w.IsRunning() && w.queue().Len() == 0 && w.curProcessing.Load() == 0 {
		return
	}

	// Otherwise, create a waiter channel and wait for it to be closed
	waiter := make(chan struct{})

	w.mx.Lock()
	w.waiters = append(w.waiters, waiter)
	w.mx.Unlock()

	<-waiter
}

// spawnWorker starts a worker goroutine to process jobs from the specified channel
// It continuously reads jobs from the channel and processes each one
// Each job processing is wrapped in its own function with proper cleanup
// Time complexity: O(1) per job
func (w *worker[T, JobType]) spawnWorker(node *linkedlist.Node[pool.Node[JobType]]) {
	for j := range node.Value.Read() {
		w.workerFunc(j)

		j.changeStatus(finished)
		j.Close()
		w.freePoolNode(node) // push back the free channel to the stack to be used for the next job
		w.releaseWaiters(w.curProcessing.Add(^uint32(0)))
		w.notifyToPullNextJobs()
	}
}

func (w *worker[T, JobType]) releaseWaiters(processing uint32) {
	// Early return if there's still processing happening
	if processing != 0 {
		return
	}

	w.mx.Lock()
	defer w.mx.Unlock()

	// Nothing to do if there are no waiters
	if len(w.waiters) == 0 {
		return
	}

	// Only release waiters if worker is paused or if running with an empty queue
	if shouldReleaseWaiters := w.IsPaused() || (w.IsRunning() && w.Queue.Len() == 0); !shouldReleaseWaiters {
		return
	}

	// Close all waiter channels to signal waiting goroutines
	for _, waiter := range w.waiters {
		close(waiter)
	}

	// Reset the waiters slice
	w.waiters = make([]chan struct{}, 0)
}

// startEventLoop starts the event loop that processes pending jobs when workers become available
// It continuously checks if the worker is running, has available capacity, and if there are jobs in the queue
// When all conditions are met, it processes the next job in the queue
func (w *worker[T, JobType]) startEventLoop() {
	for range w.eventLoopSignal {
		for w.IsRunning() && w.curProcessing.Load() < w.concurrency.Load() && w.Queue.Len() > 0 {
			w.processNextJob()
		}
	}
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

		if j, ok = v.(JobType); !ok {
			w.processNextJob()
			return
		}

		j.setInternalQueue(w.Queue)
	default:
		return
	}

	if j.IsClosed() {
		w.processNextJob()
		return
	}

	w.curProcessing.Add(1)
	j.changeStatus(processing)
	j.setAckId(ackId)

	// then job will be process by the processSingleJob function inside spawnWorker
	w.sendToNextChannel(j)
}

func (w *worker[T, JobType]) freePoolNode(node *linkedlist.Node[pool.Node[JobType]]) {
	// If worker timeout is enabled, update the last used time
	enabledIdleWorkersRemover := w.Configs.IdleWorkerExpiryDuration > 0

	if enabledIdleWorkersRemover {
		node.Value.UpdateLastUsed()
	}

	// If queue length is high or we're under our idle worker target, keep this worker
	if w.queue().Len() >= w.NumConcurrency() || enabledIdleWorkersRemover || w.pool.Len() < w.numMinIdleWorkers() {
		w.pool.PushNode(node)
		return
	}

	// Otherwise close this channel to reduce idle workers
	node.Value.Close()
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
	node := linkedlist.NewNode(pool.NewNode[JobType](1))
	// Start a worker goroutine to process jobs from this nodes channel
	go w.spawnWorker(node)

	return node
}

// notifyToPullNextJobs notifies the pullNextJobs function to process the next Job.
func (w *worker[T, JobType]) notifyToPullNextJobs() {
	w.mx.RLock()
	defer w.mx.RUnlock()

	select {
	case w.eventLoopSignal <- struct{}{}:
	default:
	}
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
		w.notifyToPullNextJobs()
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
		// since the pool node might be busy processing a job, we need to retry until we get the nodes
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

func (w *worker[T, JobType]) Pause() error {
	if !w.IsRunning() {
		return errNotRunningWorker
	}

	w.status.Store(paused)
	return nil
}

func (w *worker[T, JobType]) Stop() error {
	if !w.IsRunning() {
		return errNotRunningWorker
	}

	defer w.status.Store(stopped)

	// wait until all ongoing processes are done to gracefully close the pool nodes
	w.PauseAndWait()
	w.stopTickers()

	w.mx.Lock()
	close(w.eventLoopSignal)
	w.mx.Unlock()

	// remove all nodes from the list and close the pool nodes
	for _, node := range w.pool.NodeSlice() {
		node.Value.Close()
		w.pool.Remove(node)
	}

	return nil
}

func (w *worker[T, JobType]) Restart() error {
	// first pause the queue to avoid routine leaks or deadlocks
	// wait until all ongoing processes are done to gracefully close the pool nodes if any.
	w.PauseAndWait()

	w.mx.Lock()
	w.eventLoopSignal = make(chan struct{}, 1)
	w.mx.Unlock()

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
	if w.status.Load() == initiated {
		return w.start()
	}

	if w.IsRunning() {
		return errRunningWorker
	}

	w.status.Store(running)
	w.notifyToPullNextJobs()

	return nil
}

func (w *worker[T, JobType]) PauseAndWait() error {
	if err := w.Pause(); err != nil {
		return err
	}

	w.wait()
	return nil
}
