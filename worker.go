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
	idle
	running
	pausing
	paused
	stopping
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
	ErrWorkerPaused     = errors.New("worker is paused")
	ErrWorkerStopped    = errors.New("worker is stopped")
	ErrWorkerPausing    = errors.New("worker is pausing")
	ErrWorkerStopping   = errors.New("worker is stopping")
	ErrSameConcurrency  = errors.New("worker already has the same concurrency")
	ErrFailedToDequeue  = errors.New("failed to dequeue job")
	ErrFailedToCastJob  = errors.New("failed to cast job")
	ErrGetNextQueue     = errors.New("failed to get next queue")
	ErrParseJob         = errors.New("failed to parse job")
)

type worker[T any, JobType iJob[T]] struct {
	workerFunc      func(j JobType)
	pool            *pool.Pool[JobType]
	queues          *queueManager
	metrics         Metrics
	concurrency     atomic.Uint32
	curProcessing   atomic.Uint32
	status          atomic.Uint32
	eventLoopSignal chan struct{}
	errorChan       chan error
	waiters         *sync.Cond
	mx              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	Configs         configs
}

// Worker represents a worker that processes Jobs.
type Worker interface {
	// IsRunning returns whether the worker is actively processing jobs.
	//
	// A worker is Running when it has at least one job currently being processed.
	// This is a transient state — when no jobs are processing, the worker
	// transitions to Idle (if active) or Paused/Stopped.
	IsRunning() bool
	// IsIdle returns whether the worker is in the Idle state.
	//
	// A worker is Idle when it is active (started and not paused/stopped)
	// but has no pending jobs in the queue and no jobs currently being processed.
	// It can immediately resume processing when new jobs arrive.
	IsIdle() bool
	// IsActive returns whether the worker is either Running or Idle.
	//
	// An active worker is started and ready to process jobs. It is not
	// Paused, Stopping, Stopped, or in an intermediate transition state.
	IsActive() bool
	// IsPaused returns whether the worker is in the Paused state.
	//
	// A paused worker does not process new jobs from the queue, but any
	// in-flight jobs that were already running continue until completion.
	// Use Resume() to resume processing.
	IsPaused() bool
	// IsStopped returns whether the worker is in the Stopped state.
	//
	// A stopped worker has ceased all processing and removed all worker
	// goroutines. It cannot be resumed; use Restart() to start again.
	IsStopped() bool
	// Status returns the human-readable current status of the worker.
	//
	// Possible values: "Idle", "Running", "Pausing", "Paused",
	// "Stopping", "Stopped"
	Status() string
	// NumProcessing returns the number of jobs currently being processed
	// by the worker (in-flight jobs).
	NumProcessing() int
	// NumPending returns the number of jobs waiting in the queue
	// that have not yet started processing.
	NumPending() int
	// NumConcurrency returns the maximum number of jobs that can be
	// processed concurrently (the pool size).
	NumConcurrency() int
	// NumIdleWorkers returns the number of idle worker goroutines
	// currently available in the pool to process new jobs.
	NumIdleWorkers() int
	// Errs returns a read-only channel that receives all errors
	// encountered during job processing.
	Errs() <-chan error
	// Context returns the context associated with this worker.
	//
	// The context is cancelled when the worker is Stopped or Restarted.
	Context() context.Context
	// Metrics returns the metrics collected for this worker,
	// including submitted, completed, successful, and failed job counts.
	Metrics() Metrics
	// TunePool adjusts the concurrency (pool size) of the worker
	// up or down. This takes effect immediately for new jobs.
	TunePool(concurrency int) error
	// Pause initiates a graceful pause of the worker.
	//
	// This is non-blocking and returns immediately. The behavior depends on
	// whether there are in-flight jobs:
	//   - If no jobs are processing: transitions directly to Paused.
	//   - If jobs are processing: transitions to Pausing. In-flight jobs
	//     continue until completion, then the status becomes Paused.
	//
	// Once Paused, no new jobs are processed from the queue. Use Resume()
	// to resume processing, or PauseAndWait() to block until fully paused.
	Pause() error

	// Stop initiates a graceful shutdown of the worker.
	//
	// This is non-blocking and returns immediately. The worker transitions
	// to Stopping and cancels its internal context. The event loop then
	// waits for in-flight jobs to finish, removes all worker goroutines,
	// and transitions to Stopped.
	//
	// A stopped worker cannot be resumed; use Restart() instead.
	// Use StopAndWait() to block until fully stopped.
	Stop() error
	// Restart gracefully restarts the worker, preserving any pending
	// jobs in the queue. It stops the worker, waits for it to reach
	// Stopped, recreates the internal context, and starts again.
	Restart() error
	// Resume resumes a paused worker and begins processing pending
	// jobs from the queue. This has no effect on a stopped worker —
	// use Restart() instead.
	Resume() error
	// WaitUntilFinished waits until all pending jobs are processed and the worker
	// has no in-flight work. This is an alias for Wait().
	//
	// Deprecated: Use Wait() or WaitUntilIdle() instead, depending on whether you
	// care about the final status or just that work is done.
	WaitUntilFinished()
	// Wait blocks until the worker has no pending jobs in the queue and no jobs
	// currently being processed.
	//
	// This returns when:
	//   - Status is Running or Idle: queue is empty AND curProcessing == 0
	//   - Status is Paused or Stopped: curProcessing == 0 (all in-flight jobs done)
	//   - Status is Pausing or Stopping: curProcessing == 0
	//
	// Unlike WaitUntilIdle(), this does NOT require the status to be Idle.
	// For example, if Pause() is called and all jobs drain, Wait() returns
	// even though the status becomes Paused (not Idle).
	//
	// Use this when you only care that work is done, regardless of final status.
	// Use WaitUntilIdle() when you specifically need the Idle state.
	Wait()
	// WaitUntilIdle blocks until the worker reaches Idle status with no pending jobs.
	//
	// Unlike Wait(), which returns as soon as all in-flight jobs complete and the
	// queue is drained (regardless of final status), WaitUntilIdle specifically
	// waits for the worker's status to become Idle with an empty queue.
	//
	// This means:
	//   - If status is Running with no work, it blocks until the worker transitions to Idle
	//   - If status is Paused with no work, it blocks indefinitely (Paused != Idle)
	//   - If status is Stopped, it blocks indefinitely (Stopped != Idle)
	//
	// Use this when you need the worker in the Idle state specifically.
	// Use Wait() when you only care that work is done.
	//
	// Note: If the worker is already Idle with no pending jobs, this returns immediately.
	WaitUntilIdle()
	// WaitUntilPaused blocks until the worker reaches Paused status specifically.
	//
	// This blocks indefinitely if the worker never transitions to Paused.
	// For example, if Restart() or Stop() is called instead of Pause(),
	// the status will never become Paused and this will hang.
	//
	// Note: If the worker is already Paused, this returns immediately.
	WaitUntilPaused()
	// WaitUntilStopped blocks until the worker reaches Stopped status specifically.
	//
	// This blocks indefinitely if the worker never transitions to Stopped.
	// For example, if Pause() is called instead of Stop(), the status will
	// never become Stopped and this will hang.
	//
	// Note: If the worker is already Stopped, this returns immediately.
	WaitUntilStopped()
	// WaitAndStop waits until all pending jobs are processed and in-flight
	// jobs complete, then stops the worker and blocks until fully stopped.
	//
	// This is equivalent to calling Wait(), Stop(), and WaitUntilStopped().
	// Use this when you want to drain the queue before shutting down.
	WaitAndStop() error
	// StopAndWait initiates a graceful stop and blocks until the worker
	// reaches the Stopped state.
	//
	// This is equivalent to calling Stop() followed by WaitUntilStopped().
	// If there are in-flight jobs, this blocks until they complete and
	// all worker goroutines are removed.
	StopAndWait() error
	// WaitAndPause waits until all pending jobs are processed and in-flight
	// jobs complete, then pauses the worker.
	//
	// This is equivalent to calling Wait() followed by Pause(). Use this
	// when you want to drain the queue before pausing.
	WaitAndPause() error
	// PauseAndWait initiates a pause and blocks until the worker reaches
	// the Paused state.
	//
	// This is equivalent to calling Pause() followed by WaitUntilPaused().
	// If there are in-flight jobs, this blocks until they complete and
	// the status transitions from Pausing to Paused.
	PauseAndWait() error

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
		queues:          newQueueManager(c.strategy),
		metrics:         newMetrics(),
		eventLoopSignal: make(chan struct{}, eventLoopSignalCap),
		errorChan:       make(chan error, errorChanCap),
		Configs:         c,
	}

	w.waiters = sync.NewCond(&w.mx)
	w.concurrency.Store(c.concurrency)
	w.initContext(c.ctx)

	return w
}

func newErrWorker[T any](wf func(j iErrorJob[T]), configs ...any) *worker[T, iErrorJob[T]] {
	c := loadConfigs(configs...)

	w := &worker[T, iErrorJob[T]]{
		concurrency:     atomic.Uint32{},
		workerFunc:      wf,
		pool:            pool.New[iErrorJob[T]](poolChanCap),
		queues:          newQueueManager(c.strategy),
		metrics:         newMetrics(),
		eventLoopSignal: make(chan struct{}, eventLoopSignalCap),
		errorChan:       make(chan error, errorChanCap),
		Configs:         c,
	}

	w.waiters = sync.NewCond(&w.mx)
	w.concurrency.Store(c.concurrency)
	w.initContext(c.ctx)

	return w
}

func newResultWorker[T, R any](wf func(j iResultJob[T, R]), configs ...any) *worker[T, iResultJob[T, R]] {
	c := loadConfigs(configs...)

	w := &worker[T, iResultJob[T, R]]{
		concurrency:     atomic.Uint32{},
		workerFunc:      wf,
		pool:            pool.New[iResultJob[T, R]](poolChanCap),
		queues:          newQueueManager(c.strategy),
		metrics:         newMetrics(),
		eventLoopSignal: make(chan struct{}, eventLoopSignalCap),
		errorChan:       make(chan error, errorChanCap),
		Configs:         c,
	}

	w.waiters = sync.NewCond(&w.mx)
	w.concurrency.Store(c.concurrency)
	w.initContext(c.ctx)

	return w
}

func (w *worker[T, JobType]) initContext(parentCtx context.Context) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	w.ctx, w.cancel = context.WithCancel(parentCtx)
	w.Configs.ctx = parentCtx
}

func (w *worker[T, JobType]) configs() configs {
	return w.Configs
}

func (w *worker[T, JobType]) releaseWaiters(processing uint32) {
	// Early return if there's still processing happening
	if processing != 0 {
		return
	}

	defer w.waiters.Broadcast()

	if w.status.CompareAndSwap(running, idle) {
		return
	}

	// Transition from pausing to paused when done processing
	if w.status.CompareAndSwap(pausing, paused) {
		return
	}
}

func (w *worker[T, JobType]) sendError(err error) {
	select {
	case w.errorChan <- err:
	default:
	}
}

func (w *worker[T, JobType]) Metrics() Metrics {
	return w.metrics
}

// WaitUntilFinished waits until all pending jobs are processed.
//
// Deprecated: Use Wait() or WaitUntilIdle() instead.
func (w *worker[T, JobType]) WaitUntilFinished() {
	w.Wait()
}

// Wait blocks until the worker has no pending jobs in the queue
// and no jobs currently being processed.
//
// See the Worker interface for full documentation.
func (w *worker[T, JobType]) Wait() {
	w.mx.Lock()
	defer w.mx.Unlock()

	for {
		s := w.status.Load()
		switch s {
		case running, idle:
			if w.queues.Len() == 0 && w.curProcessing.Load() == 0 {
				return
			}
		case paused, stopped, pausing, stopping:
			if w.curProcessing.Load() == 0 {
				return
			}
		default:
			return
		}
		w.waiters.Wait()
	}
}

func (w *worker[T, JobType]) WaitUntilIdle() {
	w.mx.Lock()
	defer w.mx.Unlock()

	for {
		if w.status.Load() == idle && w.queues.Len() == 0 {
			return
		}

		w.waiters.Wait()
	}
}

func (w *worker[T, JobType]) WaitUntilPaused() {
	w.mx.Lock()
	defer w.mx.Unlock()

	for w.status.Load() != paused {
		w.waiters.Wait()
	}
}

func (w *worker[T, JobType]) WaitUntilStopped() {
	w.mx.Lock()
	defer w.mx.Unlock()

	for w.status.Load() != stopped {
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
	// if worker is idle, change the status to running
	w.status.CompareAndSwap(idle, running)

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
		w.metrics.incCompleted()
		w.freePoolNode(node)
		processing := w.curProcessing.Add(^uint32(0))
		w.releaseWaiters(processing)

		if processing < w.concurrency.Load() {
			w.notifyToPullNextJobs()
		}
	})

	return node
}

// notifyToPullNextJobs notifies the pullNextJobs function to process the next Job.
func (w *worker[T, JobType]) notifyToPullNextJobs() {
	select {
	case w.eventLoopSignal <- struct{}{}:
	default:
		// This default case means the eventLoopSignal buffer is full or
		// no one is listening. This is generally fine as it's a non-blocking send.
	}
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

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
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
		}
	}(w.ctx)
}

// starts the event loop that processes pending jobs when workers become available
// It continuously checks if the worker is running, has available capacity, and if there are jobs in the queue
// When all conditions are met, it processes the next job in the queue
func (w *worker[T, JobType]) goEventLoop() {
	go func(ctx context.Context, signal <-chan struct{}) {
		for {
			select {
			case <-ctx.Done():
				w.status.Store(stopping)
				w.Wait()
				w.removeAllWorkers()
				w.status.Store(stopped)
				w.waiters.Broadcast()
				return
			case <-signal:
				for w.IsActive() && w.curProcessing.Load() < w.concurrency.Load() && w.queues.Len() > 0 {
					if err := w.processNextJob(); err != nil {
						w.sendError(err)
					}
				}
			}
		}
	}(w.ctx, w.eventLoopSignal)
}

// removeAllWorkers removes all nodes from the list and closes the pool nodes
func (w *worker[T, JobType]) removeAllWorkers() {
	for _, node := range w.pool.NodeSlice() {
		w.pool.Remove(node)
		node.Value.Stop()
		w.pool.Cache.Put(node)
	}
}

func (w *worker[T, JobType]) TunePool(concurrency int) error {
	if !w.IsActive() {
		return w.getStatusError()
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

func (w *worker[T, JobType]) NumPending() int {
	return w.queues.Len()
}

func (w *worker[T, JobType]) Pause() error {
	s := w.status.Load()

	if s == paused || s == pausing {
		return nil
	}

	if w.status.CompareAndSwap(idle, paused) {
		return nil
	}

	if w.status.CompareAndSwap(running, pausing) {
		if w.NumProcessing() == 0 {
			w.status.CompareAndSwap(pausing, paused)
		}
		return nil
	}

	return w.getStatusError()
}

func (w *worker[T, JobType]) Resume() error {
	if w.status.CompareAndSwap(paused, idle) {
		w.notifyToPullNextJobs()
		return nil
	}

	return w.getStatusError()
}

func (w *worker[T, JobType]) start() error {
	if w.IsActive() {
		return ErrRunningWorker
	}

	defer w.notifyToPullNextJobs()
	defer w.status.Store(idle)

	w.goEventLoop()
	w.goRemoveIdleWorkers()

	// init the first worker by default
	w.pool.PushNode(w.initPoolNode())

	return nil
}

func (w *worker[T, JobType]) Stop() error {
	s := w.status.Load()

	if s == stopped || s == stopping {
		return nil
	}

	for _, from := range []status{running, idle, pausing, paused} {
		if w.status.CompareAndSwap(from, stopping) {
			w.cancel()
			return nil
		}
	}

	return w.getStatusError()
}

func (w *worker[T, JobType]) Restart() error {
	if err := w.Stop(); err != nil {
		return err
	}

	w.WaitUntilStopped()

	w.ctx, w.cancel = context.WithCancel(w.Configs.ctx)

	return w.start()
}

func (w *worker[T, JobType]) IsPaused() bool {
	return w.status.Load() == paused
}

func (w *worker[T, JobType]) IsRunning() bool {
	return w.status.Load() == running
}

func (w *worker[T, JobType]) IsActive() bool {
	s := w.status.Load()
	return s == running || s == idle
}

func (w *worker[T, JobType]) IsIdle() bool {
	return w.status.Load() == idle
}

func (w *worker[T, JobType]) IsStopped() bool {
	return w.status.Load() == stopped
}

func (w *worker[T, JobType]) getStatusError() error {
	switch s := w.status.Load(); s {
	case paused:
		return ErrWorkerPaused
	case stopped:
		return ErrWorkerStopped
	case pausing:
		return ErrWorkerPausing
	case stopping:
		return ErrWorkerStopping
	case running, idle:
		return ErrRunningWorker
	default:
		return ErrNotRunningWorker
	}
}

func (w *worker[T, JobType]) Status() string {
	switch w.status.Load() {
	case initiated:
		return "Initiated"
	case idle:
		return "Idle"
	case running:
		return "Running"
	case pausing:
		return "Pausing"
	case paused:
		return "Paused"
	case stopping:
		return "Stopping"
	case stopped:
		return "Stopped"
	default:
		return "Unknown"
	}
}

func (w *worker[T, JobType]) NumProcessing() int {
	return int(w.curProcessing.Load())
}

func (w *worker[T, JobType]) Context() context.Context {
	return w.ctx
}

func (w *worker[T, JobType]) PauseAndWait() error {
	if err := w.Pause(); err != nil {
		return err
	}

	w.WaitUntilPaused()
	return nil
}

func (w *worker[T, JobType]) WaitAndPause() error {
	w.Wait()

	return w.Pause()
}

func (w *worker[T, JobType]) StopAndWait() error {
	if err := w.Stop(); err != nil {
		return err
	}

	w.WaitUntilStopped()
	return nil
}

func (w *worker[T, JobType]) WaitAndStop() error {
	w.Wait()

	if err := w.Stop(); err != nil {
		return err
	}

	w.WaitUntilStopped()
	return nil
}
