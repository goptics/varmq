package varmq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrDetached is the outcome of a job on shared storage that can't report
// outcomes (no OutcomeWatcher), or whose handle was closed: it may still
// run, but this handle never learns how it ended.
var ErrDetached = errors.New("varmq: job outcome unknown to this handle")

// RemoteError is a failed job's error as it came back from shared storage:
// its text only. errors.Is/As with the worker's own error types does not
// match across processes.
type RemoteError struct{ Reason string }

func (e *RemoteError) Error() string { return e.Reason }

// remoteJob is the handle of a job on shared storage: any process may run
// it, and the handle learns the outcome from the storage (OutcomeWatcher).
// It is every Enqueued*Job kind; R decodes a result worker's result.
type remoteJob[R any] struct {
	id     string
	done   chan struct{}
	once   sync.Once
	out    JobOutcome
	err    error // ErrDetached: no outcome will come
	cancel func()
	onEnd  func(*remoteJob[R]) // set before the watch; runs once, at the end
}

// watchRemote is the handle of job id on storage s: watched when s reports
// outcomes, detached otherwise. onEnd, if any, runs once at its end.
func watchRemote[R any](s any, id string, onEnd func(*remoteJob[R])) *remoteJob[R] {
	j := &remoteJob[R]{id: id, done: make(chan struct{}), onEnd: onEnd}

	ow, ok := s.(OutcomeWatcher)
	if !ok || id == "" {
		j.end(JobOutcome{}, ErrDetached)
		return j
	}

	j.cancel = ow.WatchOutcome(id, func(o JobOutcome) { j.end(o, nil) })
	return j
}

func (j *remoteJob[R]) end(o JobOutcome, err error) {
	j.once.Do(func() {
		j.out, j.err = o, err
		close(j.done)
		if j.onEnd != nil {
			j.onEnd(j)
		}
	})
}

func (j *remoteJob[R]) ID() string { return j.id }

func (j *remoteJob[R]) IsClosed() bool {
	select {
	case <-j.done:
		return true
	default:
		return false
	}
}

// Status is "Queued" until the outcome (or Close), then "Closed".
func (j *remoteJob[R]) Status() string {
	if j.IsClosed() {
		return "Closed"
	}
	return "Queued"
}

func (j *remoteJob[R]) Wait() { <-j.done }

func (j *remoteJob[R]) WaitContext(ctx context.Context) error {
	select {
	case <-j.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops waiting: Wait returns and Err reports ErrDetached. It does not
// cancel the job, which still runs on its storage.
func (j *remoteJob[R]) Close() error {
	if j.cancel != nil {
		j.cancel()
	}
	j.end(JobOutcome{}, ErrDetached)
	return nil
}

// Err waits, then returns nil for a completed job, *RemoteError for a
// failed one, ErrDetached when no outcome came.
func (j *remoteJob[R]) Err() error {
	j.Wait()
	return outcomeErr(j.out, j.err)
}

// Result waits, then decodes the result worker's R. A completed job with no
// result (R over 64 KiB, or the worker's storage dropped it): ErrNoResult.
func (j *remoteJob[R]) Result() (R, error) {
	j.Wait()
	return decodeOutcome[R](j.out, j.err)
}

// Drain is a no-op: a single job has no channel.
func (j *remoteJob[R]) Drain() {}

func outcomeErr(o JobOutcome, err error) error {
	switch {
	case err != nil:
		return err
	case o.State == JobFailed:
		return &RemoteError{Reason: o.Error}
	}
	return nil
}

func decodeOutcome[R any](o JobOutcome, err error) (R, error) {
	var r R
	if err := outcomeErr(o, err); err != nil {
		return r, err
	}
	if len(o.Result) == 0 {
		return r, ErrNoResult
	}
	return r, json.Unmarshal(o.Result, &r)
}

// OpenJob is the handle of job id on s from any process (a restarted
// producer, another service): WaitContext, and Result, which decodes a
// ResultWorker's R and returns the job's error (*RemoteError) for any
// worker kind (use any as R for Worker / ErrWorker jobs).
// A job that ended earlier is found while s keeps its record. Close it to
// stop waiting on a job that may never end.
func OpenJob[R any](s OutcomeWatcher, id string) EnqueuedResultJob[R] {
	return watchRemote[R](s, id, nil)
}

// newRemoteID is a job ID for a durable job added without one, so its
// outcome can be matched. Random, not a counter: a counter restarts with the
// process and would match an old record.
func newRemoteID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails (Go 1.24+)
	return hex.EncodeToString(b[:])
}

// remoteGroup is AddAll's handle on shared storage: one watched job per
// item, fanned into the group's channel.
// ponytail: one goroutine per item while it waits; a shared waiter if
// groups get big.
type remoteGroup[R any] struct {
	wg      sync.WaitGroup
	n       atomic.Int64
	errs    chan error
	results chan Result[R]
}

func newRemoteGroup[R any](size int) *remoteGroup[R] {
	return &remoteGroup[R]{errs: make(chan error, size), results: make(chan Result[R], size)}
}

// track waits for j in the background and reports its end to the group.
func (g *remoteGroup[R]) track(j *remoteJob[R]) {
	g.wg.Add(1)
	g.n.Add(1)
	go func() {
		defer g.wg.Done()
		r, err := j.Result()
		if errors.Is(err, ErrNoResult) {
			err = nil
		}
		g.n.Add(-1)
		if err != nil {
			g.errs <- err
		}
		g.results <- Result[R]{JobId: j.id, Data: r, Err: err}
	}()
}

// seal closes both channels once every tracked job ended, as an in-memory
// group's channel closes, so a range over them ends. Called after the last
// track.
func (g *remoteGroup[R]) seal() {
	go func() {
		g.wg.Wait()
		close(g.errs)
		close(g.results)
	}()
}

func (g *remoteGroup[R]) NumPending() int { return int(g.n.Load()) }
func (g *remoteGroup[R]) Wait()           { g.wg.Wait() }

// Errs gets each failed item's error (*RemoteError or ErrDetached); closed
// when the group ended.
func (g *remoteGroup[R]) Errs() <-chan error { return g.errs }

// Results gets every item's Result; closed when the group ended.
func (g *remoteGroup[R]) Results() <-chan Result[R] { return g.results }

// Drain discards what both channels get, until they close.
func (g *remoteGroup[R]) Drain() {
	go func() {
		for range g.errs {
		}
	}()
	go func() {
		for range g.results {
		}
	}()
}
