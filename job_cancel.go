package varmq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

var (
	// ErrJobCancelled is the error of a job cancelled before it ran, or
	// while it ran and then returned an error (or panicked).
	ErrJobCancelled = errors.New("varmq: job cancelled")
	// ErrCancelUnsupported is Cancel's error on a job whose storage can't
	// cancel jobs by ID (no JobCanceller).
	ErrCancelUnsupported = errors.New("varmq: storage can't cancel jobs")
)

// cancelReason is the reason a cancelled job's storage records.
const cancelReason = "varmq: job cancelled"

// jobRun is the state of a running job that asked for its Context or was
// cancelled. Kept in runs, off the job struct, so a job that uses neither
// pays nothing and every job kind stays in its size class.
type jobRun struct {
	ctx       context.Context
	cancel    context.CancelFunc
	cancelled atomic.Bool
}

// runs maps a *job[T] (as any) to its *jobRun while it runs. liveRuns
// counts the entries, so a job's run end skips the map while it is empty.
// ponytail: one process-wide map; shard it if Context-heavy workloads
// contend on it.
var (
	runs     sync.Map
	liveRuns atomic.Int64
)

// runOf is the run state of j, created on first use. Only for a job that
// is processing: one made for a job that already ended is dropped and nil
// returned, so no entry outlives its run.
func (j *job[T]) runOf() *jobRun {
	if r, ok := runs.Load(j); ok {
		return r.(*jobRun)
	}
	if j.status.Load() != processing {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &jobRun{ctx: ctx, cancel: cancel}
	if old, loaded := runs.LoadOrStore(j, r); loaded {
		cancel()
		return old.(*jobRun)
	}
	liveRuns.Add(1)

	// The run loop sets finished before it ends the run: an entry stored
	// after that end would never be dropped.
	if j.status.Load() != processing {
		j.endRun()
		return nil
	}

	return r
}

// Context is the job's context while it runs: Cancel cancels it. A worker
// that watches it (ctx.Done(), or passes it to I/O) stops a cancelled job;
// one that ignores it runs to the end. Made on the first call; outside a
// run it is context.Background().
// ponytail: not derived from the worker's context, so Stop does not cancel
// it; derive it if running jobs should stop with their worker.
func (j *job[T]) Context() context.Context {
	if r := j.runOf(); r != nil {
		return r.ctx
	}
	return context.Background()
}

// endRun drops j's run state, if any, and releases its context. The run
// loop calls it after the job's status moved to finished.
func (j *job[T]) endRun() {
	if liveRuns.Load() == 0 {
		return
	}
	if r, ok := runs.LoadAndDelete(j); ok {
		liveRuns.Add(-1)
		r.(*jobRun).cancel()
	}
}

// cancelRequested reports whether Cancel reached j while it ran.
func (j *job[T]) cancelRequested() bool {
	if liveRuns.Load() == 0 {
		return false
	}
	r, ok := runs.Load(j)
	return ok && r.(*jobRun).cancelled.Load()
}

// waitingOnly allows a move from created or queued only: Cancel's close.
func waitingOnly(s status) error {
	switch s {
	case created, queued:
		return nil
	case processing:
		return ErrJobProcessing
	}
	return ErrJobAlreadyClosed
}

// cancel is Cancel with set, which stores a waiting job's
// ErrJobCancelled for its Err or Result, and release, which releases its
// waiters as the job kind's Close does.
func (j *job[T]) cancel(set, release func()) error {
	err := j.transition(closed, waitingOnly)
	if err == nil {
		// Won against startProcessing: the job never runs. Shared storage
		// and workflow stores learn it ended cancelled, so the item is not
		// redelivered and the workflow moves on.
		ackErr := j.finishCancelled()
		if set != nil {
			set()
		}
		release()
		return ackErr
	}
	if !errors.Is(err, ErrJobProcessing) {
		return err
	}

	r := j.runOf()
	if r == nil { // ended meanwhile
		return ErrJobAlreadyClosed
	}
	r.cancelled.Store(true)
	r.cancel()
	return nil
}

// Cancel stops the job. One not started yet is closed and never runs. A
// running one has its Context cancelled: it ends cancelled if it then
// returns an error or panics, and as usual if it finishes anyway, or if
// its function already returned. ErrJobAlreadyClosed once it ended.
func (j *job[T]) Cancel() error { return j.cancel(nil, j.wg.Done) }

// Cancel is job.Cancel; a job cancelled before it ran reports
// ErrJobCancelled from Err.
func (ej *errorJob[T]) Cancel() error {
	return ej.cancel(func() { ej.err = ErrJobCancelled }, ej.wg.Done)
}

// Cancel is job.Cancel; a job cancelled before it ran reports
// ErrJobCancelled from Result.
func (rj *resultJob[T, R]) Cancel() error {
	return rj.cancel(func() { rj.err = ErrJobCancelled }, rj.wg.Done)
}

// endFailed records a run that returned err (or panicked): cancelled when
// Cancel reached it while it ran, failed otherwise. It returns the error
// the job reports. A Cancel after this read leaves the outcome as is.
func (j *job[T]) endFailed(err error) error {
	if !j.cancelRequested() {
		j.fail(err.Error())
		return err
	}

	j.endCancelled()
	if ref := j.ackRef.Load(); ref != nil && ref.totals != nil {
		ref.totals.failed.Add(1)
	}

	return ErrJobCancelled
}

// endCancelled records that the job ended cancelled, for Close or
// finishCancelled to report: a workflow task's end, or storage's
// cancelled finish (CancelAcknowledgeable, else Reject).
func (j *job[T]) endCancelled() {
	reason := cancelReason
	ref := j.ackRef.Load()
	switch {
	case ref == nil:
	case ref.ext != nil && ref.ext.task != nil:
		j.endTask(OutcomeCancelled, reason, nil)
	case ref.q != nil:
		j.ackRef.Store(&jobAck{q: ref.q, id: ref.id, ext: &ackExt{reason: &reason, cancelled: true}})
	}
}

// finishCancelled reports a job Cancel closed before it ran: to its
// storage (acknowledge) or its in-memory workflow store. Nothing for a
// plain in-memory job.
func (j *job[T]) finishCancelled() error {
	ref := j.ackRef.Load()
	if ref == nil || (ref.q == nil && (ref.ext == nil || ref.ext.task == nil)) {
		return nil
	}

	j.endCancelled()
	if ref.q != nil {
		return j.acknowledge()
	}
	if r := j.ackRef.Load(); r != nil && r.ext != nil && r.ext.task != nil {
		finishMemTask(r.ext)
	}
	return nil
}

// Cancel on a group member releases the group as its Close does.
func (gj *groupJob[T]) Cancel() error {
	return gj.cancel(nil, func() { gj.wgc.Done() })
}

func (gj *errorGroupJob[T]) Cancel() error {
	return gj.cancel(nil, func() {
		if gj.wgc.Done() {
			gj.resp.Close()
		}
	})
}

func (gj *resultGroupJob[T, R]) Cancel() error {
	return gj.cancel(nil, func() {
		if gj.wgc.Done() {
			gj.resp.Close()
		}
	})
}

// trackRun lists j by ID while it runs, when this worker watches a
// storage's cancel requests, so a request from any process finds it. It
// returns the ID to untrack, "" when j is not tracked.
func (w *worker[T, JobType]) trackRun(j JobType) string {
	if !w.cancelWatch.Load() {
		return ""
	}
	id := j.ID()
	if id != "" {
		w.running.Store(id, j)
	}
	return id
}

// untrackRun drops j's entry, not another job's under the same ID (a
// redelivered item, a reused ID).
func (w *worker[T, JobType]) untrackRun(id string, j JobType) {
	if id != "" {
		w.running.CompareAndDelete(id, j)
	}
}

// watchCancels makes s's cancel requests (CancelNotifier) cancel this
// worker's running job of that ID. A job the worker took into a batch but
// has not started is not tracked: it runs, as one already handed out.
func (w *worker[T, JobType]) watchCancels(s any) {
	cn, ok := s.(CancelNotifier)
	if !ok {
		return
	}
	w.cancelWatch.Store(true)
	cn.OnCancel(func(id string) {
		if j, ok := w.running.Load(id); ok {
			_ = j.(JobType).Cancel()
		}
	})
}
