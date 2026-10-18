package varmq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goptics/varmq/internal/pool"
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
	ErrParseJob         = errors.New("failed to parse job")
)

type worker[T any, JobType iJob[T]] struct {
	workerFunc func(j JobType)
	pool       *pool.Pool[JobType]
	// sources holds the current sourceManager (empty, direct or strategy) in
	// a managerBox. The event loop only loads it; sourcesMx serialises the
	// add/remove calls that swap it.
	sources         atomic.Value
	sourcesMx       sync.Mutex
	metrics         Metrics
	concurrency     atomic.Uint32
	curProcessing   atomic.Uint32 // jobs taken from the sources and not yet finished
	busyWorkers     atomic.Uint32 // pool goroutines serving a batch; bounded by concurrency
	jobNanos        atomic.Int64  // ns per job in the last finished batch; 0 until one has run
	dispatched      uint64        // jobs handed out in batches that can be parked; event loop only
	eventLoopSignal chan struct{}
	errorChan       chan error
	waiters         *sync.Cond
	mx              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	Configs         configs
	registryTimer   *time.Timer
	// status is read before every job, so it sits apart from the per-batch
	// counters above. The park fields share its line: they change only on a
	// stop or pause and while parked jobs are handed out again.
	status       atomic.Uint32
	nParked      atomic.Int64 // len of all parked batches, read without parkMx
	parkMx       sync.Mutex
	parked       []parkedBatch[JobType] // unstarted jobs set aside by Stop or Pause
	parkUnsorted bool                   // parked gained a batch since unpark last sorted it by Seq
	hookOnce     sync.Once
	hook         *parkHook // how a Purge reaches parked; storage holds it weakly
}

// Worker represents a worker that processes Jobs.
type Worker interface {
	// Name returns the unique name of the worker.
	Name() string
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
	// Possible values: "Initiated", "Idle", "Running", "Pausing", "Paused",
	// "Stopping", "Stopped"
	Status() string
	// NumProcessing returns the number of jobs being executed right now,
	// never more than NumConcurrency.
	NumProcessing() int
	// NumPending returns the number of jobs that have not started: those
	// still in the bound queues plus those already handed to a worker in a
	// batch and waiting their turn, or parked by a Stop or Pause. A bound
	// queue's own Len counts only the former. A job closed while it waits in
	// storage, a batch or the park counts until the worker reaches and
	// discards it. Submitted == NumPending + NumProcessing + Completed, except
	// for an instant while a stopping worker parks its batch.
	NumPending() int
	// NumConcurrency returns the upper bound on jobs processed at once. It
	// is a limit, not a pool size: goroutines are spawned on demand and only
	// while another one would shorten the backlog by more than it costs.
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
	//
	// Errors:
	//   - ErrSameConcurrency: worker already has this concurrency value.
	//   - see getStatusError() for additional state-specific errors.
	TunePool(concurrency int) error
	// Pause initiates a graceful pause of the worker.
	//
	// This is non-blocking and returns immediately. The behavior depends on
	// whether there are in-flight jobs:
	//   - If no jobs are processing: transitions directly to Paused.
	//   - If jobs are processing: transitions to Pausing. In-flight jobs
	//     continue until completion, then the status becomes Paused. Jobs a
	//     worker holds in its batch but has not started are parked on the
	//     worker: still Queued and counted by NumPending, and handed out
	//     first, in order, on Resume.
	//
	// Once Paused, no new jobs are processed from the queue. Use Resume()
	// to resume processing, or PauseAndWait() to block until fully paused.
	//
	// Pause is idempotent: calling Pause on an already Paused or Pausing
	// worker returns nil.
	//
	// Errors:
	//   - ErrWorkerStopped: worker is in the Stopped state.
	//   - ErrWorkerStopping: worker is in the Stopping state.
	//   - ErrNotRunningWorker: worker is in the Initiated state.
	Pause() error

	// Stop initiates a graceful shutdown of the worker.
	//
	// This is non-blocking and returns immediately. The worker transitions
	// to Stopping and cancels its internal context. The event loop then
	// waits for in-flight jobs to finish, removes all worker goroutines,
	// and transitions to Stopped. Jobs a worker holds in its batch but has
	// not started are parked on the worker: still Queued and counted by
	// NumPending, and handed out first, in order, on Restart.
	//
	// A stopped worker cannot be resumed; use Restart() instead.
	// Use StopAndWait() to block until fully stopped.
	//
	// Stop is idempotent: calling Stop on an already Stopped or Stopping
	// worker returns nil.
	//
	// Errors:
	//   - ErrNotRunningWorker: worker is in the Initiated state.
	Stop() error
	// Restart gracefully restarts the worker, preserving any pending
	// jobs in the queue. It stops the worker, waits for it to reach
	// Stopped, recreates the internal context, and starts again.
	//
	// Restart works from any valid state: Running, Idle, Pausing, Paused,
	// Stopping, or Stopped. It transparently handles the case where another
	// goroutine already restarted the worker by returning ErrRunningWorker.
	//
	// Errors:
	//   - ErrRunningWorker: worker is already active (concurrent restart).
	//   - ErrNotRunningWorker: worker is in the Initiated state.
	//   - Errors from Stop(): see Stop's error documentation.
	Restart() error
	// Resume resumes a paused worker and begins processing pending
	// jobs from the queue.
	//
	// Resume is idempotent: calling Resume on an already active (Running or
	// Idle) worker returns nil.
	//
	// Errors:
	//   - ErrWorkerStopped: worker is in the Stopped state. Use Restart().
	//   - ErrWorkerStopping: worker is in the Stopping state.
	//   - ErrWorkerPausing: worker is in the Pausing state.
	//   - ErrNotRunningWorker: worker is in the Initiated state.
	Resume() error
	// Start begins processing jobs from the bound queues.
	// When WithAutoRun is true (the default), the worker starts automatically
	// after creation and this method returns ErrRunningWorker.
	// Use WithAutoRun(false) to create a worker in an initiated state,
	// then call Start() manually when ready.
	//
	// Start only accepts initiated or stopped states. For all other states
	// that still have a running event loop, it returns the corresponding
	// status error.
	//
	// Errors:
	//   - ErrRunningWorker: worker is already active (Running or Idle).
	//   - ErrWorkerPaused: worker is in the Paused state. Use Resume().
	//   - ErrWorkerStopping: worker is in the Stopping state.
	//   - ErrWorkerPausing: worker is in the Pausing state.
	Start() error
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
	// jobs complete, then initiates a graceful stop and returns immediately.
	//
	// This is equivalent to calling Wait() followed by Stop().
	// Use this when you want to drain the queue before shutting down.
	// Use StopAndWait() when you need to block until the worker reaches
	// the Stopped state.
	//
	// Errors:
	//   - see Stop() for error documentation.
	WaitAndStop() error
	// StopAndWait initiates a graceful stop and blocks until the worker
	// reaches the Stopped state.
	//
	// This is equivalent to calling Stop() followed by WaitUntilStopped().
	// If there are in-flight jobs, this blocks until they complete and
	// all worker goroutines are removed.
	//
	// Errors:
	//   - see Stop() for error documentation.
	StopAndWait() error
	// WaitAndPause waits until all pending jobs are processed and in-flight
	// jobs complete, then pauses the worker.
	//
	// This is equivalent to calling Wait() followed by Pause(). Use this
	// when you want to drain the queue before pausing.
	//
	// Errors:
	//   - see Pause() for error documentation.
	WaitAndPause() error
	// PauseAndWait initiates a pause and blocks until the worker reaches
	// the Paused state.
	//
	// This is equivalent to calling Pause() followed by WaitUntilPaused().
	// If there are in-flight jobs, this blocks until they complete and
	// the status transitions from Pausing to Paused.
	//
	// Errors:
	//   - see Pause() for error documentation.
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
		metrics:         newMetrics(),
		eventLoopSignal: make(chan struct{}, eventLoopSignalCap),
		errorChan:       make(chan error, errorChanCap),
		Configs:         c,
	}

	w.sources.Store(managerBox[iJob[T]]{newSourceManager[iJob[T]]()})
	w.waiters = sync.NewCond(&w.mx)
	w.concurrency.Store(c.concurrency)
	w.initContext(c.ctx)

	if c.autoRun {
		w.Start()
	}

	return w
}

func newErrWorker[T any](wf func(j iErrorJob[T]), configs ...any) *worker[T, iErrorJob[T]] {
	c := loadConfigs(configs...)

	w := &worker[T, iErrorJob[T]]{
		concurrency:     atomic.Uint32{},
		workerFunc:      wf,
		pool:            pool.New[iErrorJob[T]](poolChanCap),
		metrics:         newMetrics(),
		eventLoopSignal: make(chan struct{}, eventLoopSignalCap),
		errorChan:       make(chan error, errorChanCap),
		Configs:         c,
	}

	w.sources.Store(managerBox[iErrorJob[T]]{newSourceManager[iErrorJob[T]]()})
	w.waiters = sync.NewCond(&w.mx)
	w.concurrency.Store(c.concurrency)
	w.initContext(c.ctx)

	if c.autoRun {
		w.Start()
	}

	return w
}

func newResultWorker[T, R any](wf func(j iResultJob[T, R]), configs ...any) *worker[T, iResultJob[T, R]] {
	c := loadConfigs(configs...)

	w := &worker[T, iResultJob[T, R]]{
		concurrency:     atomic.Uint32{},
		workerFunc:      wf,
		pool:            pool.New[iResultJob[T, R]](poolChanCap),
		metrics:         newMetrics(),
		eventLoopSignal: make(chan struct{}, eventLoopSignalCap),
		errorChan:       make(chan error, errorChanCap),
		Configs:         c,
	}

	w.sources.Store(managerBox[iResultJob[T, R]]{newSourceManager[iResultJob[T, R]]()})
	w.waiters = sync.NewCond(&w.mx)
	w.concurrency.Store(c.concurrency)
	w.initContext(c.ctx)

	if c.autoRun {
		w.Start()
	}

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

func (w *worker[T, JobType]) sendError(err error) {
	select {
	case w.errorChan <- err:
	default:
	}
	w.Configs.errHandler(w, err)
}

func (w *worker[T, JobType]) Name() string {
	return w.Configs.name
}

func (w *worker[T, JobType]) Context() context.Context {
	return w.ctx
}

func (w *worker[T, JobType]) Metrics() Metrics {
	return w.metrics
}

func (w *worker[T, JobType]) Errs() <-chan error {
	return w.errorChan
}

func (w *worker[T, JobType]) NumPending() int {
	// Each busy worker runs one job, so taken minus running sits in batch
	// buffers. The loads are not a snapshot: clamp at 0. Jobs a stopping worker
	// just parked count twice (parked and still taken) until its batch ends.
	taken, running := w.curProcessing.Load(), w.busyWorkers.Load()
	buffered := 0
	if taken > running {
		buffered = int(taken - running)
	}

	return w.pending(w.manager()) + buffered
}

func (w *worker[T, JobType]) NumProcessing() int {
	return int(w.busyWorkers.Load())
}

func (w *worker[T, JobType]) NumConcurrency() int {
	return int(w.concurrency.Load())
}

func (w *worker[T, JobType]) NumIdleWorkers() int {
	return w.pool.Len()
}
