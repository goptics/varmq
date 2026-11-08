package varmq

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goptics/varmq/contract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancelWaitingJob(t *testing.T) {
	ran := make(chan int, 2)
	w := NewErrWorker(func(j Job[int]) error { ran <- j.Data(); return nil })
	w.Pause()
	defer w.Stop()

	q := w.BindQueue()
	j, _ := q.Add(1)
	require.NoError(t, j.Cancel())
	assert.ErrorIs(t, j.Err(), ErrJobCancelled)
	assert.ErrorIs(t, j.Cancel(), ErrJobAlreadyClosed)

	w.Resume()
	j2, _ := q.Add(2)
	require.NoError(t, j2.Err())
	assert.Equal(t, 2, <-ran, "the cancelled job never ran")
}

func TestCancelRunningJob(t *testing.T) {
	started := make(chan struct{})
	w := NewResultWorker(func(j Job[int]) (int, error) {
		close(started)
		<-j.Context().Done()
		return 0, j.Context().Err()
	})
	defer w.Stop()

	j, _ := w.BindQueue().Add(1)
	<-started
	require.NoError(t, j.Cancel())
	_, err := j.Result()
	assert.ErrorIs(t, err, ErrJobCancelled)
	assert.Zero(t, liveRuns.Load(), "run state dropped")
}

func TestCancelIgnoredFinishesNormally(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	w := NewResultWorker(func(j Job[int]) (int, error) {
		close(started)
		<-release // never looks at Context
		return 7, nil
	})
	defer w.Stop()

	j, _ := w.BindQueue().Add(1)
	<-started
	require.NoError(t, j.Cancel())
	close(release)
	r, err := j.Result()
	require.NoError(t, err)
	assert.Equal(t, 7, r)
	assert.Zero(t, liveRuns.Load())
}

func TestContextOutsideRun(t *testing.T) {
	j := newJob(1, jobConfigs{})
	assert.Equal(t, context.Background(), j.Context())
	assert.Zero(t, liveRuns.Load())
}

// cancelStore is sharedStore with cancel by ID, as redisq does it: a mark
// that drops the job at pop (not a workflow item), a notice to consumers, a
// cancelled finish; a re-add clears the id's mark and outcome (spec 6.13,
// 6.14 Re-add).
type cancelStore struct {
	*sharedStore
	cmu    sync.Mutex
	marked map[string]bool
	hooks  []*func(string)
}

func newCancelStore() *cancelStore {
	return &cancelStore{sharedStore: newSharedStore(), marked: map[string]bool{}}
}

func (c *cancelStore) CancelJob(id string) error {
	c.mu.Lock()
	_, ended := c.outcomes[id]
	c.mu.Unlock()
	if ended {
		return contract.ErrJobEnded
	}
	c.cmu.Lock()
	c.marked[id] = true
	var hooks []func(string)
	for _, p := range c.hooks {
		hooks = append(hooks, *p)
	}
	c.cmu.Unlock()
	for _, fn := range hooks {
		fn(id)
	}
	return nil
}

func (c *cancelStore) OnCancel(fn func(string)) func() {
	p := &fn
	c.cmu.Lock()
	c.hooks = append(c.hooks, p)
	c.cmu.Unlock()
	return func() {
		c.cmu.Lock()
		defer c.cmu.Unlock()
		c.hooks = slices.DeleteFunc(c.hooks, func(x *func(string)) bool { return x == p })
	}
}

func (c *cancelStore) numHooks() int {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	return len(c.hooks)
}

func (c *cancelStore) EnqueueOnce(item any, id string) bool {
	c.mu.Lock()
	delete(c.outcomes, id)
	c.mu.Unlock()
	c.cmu.Lock()
	delete(c.marked, id)
	c.cmu.Unlock()
	return c.push(item, 0)
}

func (c *cancelStore) DequeueWithAckId() (any, bool, string) {
	for {
		v, ok, ack := c.sharedStore.DequeueWithAckId()
		if !ok {
			return v, ok, ack
		}
		var env struct {
			ID string `json:"id"`
			WF string `json:"wf"`
		}
		_ = json.Unmarshal(v.([]byte), &env)
		if env.WF != "" { // a workflow item is handed out: Workflow.Cancel stops it
			return v, ok, ack
		}
		c.cmu.Lock()
		drop := c.marked[env.ID]
		delete(c.marked, env.ID)
		c.cmu.Unlock()
		if !drop {
			return v, ok, ack
		}
		c.finish(ack, JobCancelled, "cancelled", nil)
	}
}

func (c *cancelStore) Dequeue() (any, bool) { v, ok, _ := c.DequeueWithAckId(); return v, ok }

func (c *cancelStore) AcknowledgeCancelled(ackID, reason string) bool {
	c.mu.Lock()
	id := c.pending[ackID]
	c.mu.Unlock()
	c.cmu.Lock()
	delete(c.marked, id)
	c.cmu.Unlock()
	return c.finish(ackID, JobCancelled, reason, nil)
}

var (
	_ JobCanceller          = (*cancelStore)(nil)
	_ CancelNotifier        = (*cancelStore)(nil)
	_ CancelAcknowledgeable = (*cancelStore)(nil)
)

func TestRemoteCancelRunning(t *testing.T) {
	s := newCancelStore()
	started := make(chan struct{})
	w := NewErrWorker(func(j Job[int]) error {
		close(started)
		<-j.Context().Done()
		return j.Context().Err()
	})
	defer w.Stop()
	w.WithQueue(s) // consumer

	j, _ := NewErrQueue[int](s).Add(1) // producer
	<-started
	require.NoError(t, OpenErrJob(s, j.ID()).Cancel(), "from any handle")
	assert.ErrorIs(t, j.Err(), ErrJobCancelled)
	assert.ErrorIs(t, j.Cancel(), ErrJobAlreadyClosed)
}

func TestRemoteCancelWaiting(t *testing.T) {
	s := newCancelStore()
	p := NewErrQueue[int](s)
	j, _ := p.Add(1) // no consumer yet
	require.NoError(t, j.Cancel())

	ran := make(chan int, 1)
	w := NewErrWorker(func(j Job[int]) error { ran <- j.Data(); return nil })
	defer w.Stop()
	w.WithQueue(s)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, j.WaitContext(ctx))
	assert.ErrorIs(t, j.Err(), ErrJobCancelled)
	assert.Empty(t, ran, "dropped at pop, never ran")
}

func TestRemoteCancelUnsupported(t *testing.T) {
	j, _ := NewQueue[int](newSharedStore()).Add(1)
	assert.ErrorIs(t, j.Cancel(), ErrCancelUnsupported)
}

func TestCancelWaitingDurableJobFinishesInStorage(t *testing.T) {
	s := newCancelStore()
	h, _ := NewErrQueue[int](s).Add(1) // producer handle
	_, ok, ack := s.sharedStore.DequeueWithAckId()
	require.True(t, ok)

	// the consumer's copy, taken but not started (as in a worker's batch)
	j := newErrorJob(1, jobConfigs{Id: h.ID()})
	j.setAck(s, ack)
	j.markQueued()

	require.NoError(t, j.Cancel())
	assert.ErrorIs(t, j.Err(), ErrJobCancelled)
	assert.ErrorIs(t, h.Err(), ErrJobCancelled, "storage finished it cancelled: no redelivery")
}

func TestRemoteCancelScopedToItsStorage(t *testing.T) {
	a, b := newCancelStore(), newCancelStore()
	started := make(chan struct{})
	release := make(chan struct{})
	w := NewErrWorker(func(j Job[int]) error {
		close(started)
		select {
		case <-j.Context().Done():
			return j.Context().Err()
		case <-release:
			return nil
		}
	})
	defer w.Stop()
	w.WithQueue(a)
	w.WithQueue(b)

	jb, _ := NewErrQueue[int](b).Add(1, WithJobId("same"))
	<-started
	require.NoError(t, OpenErrJob(a, "same").Cancel(), "a request on queue a")
	close(release)
	assert.NoError(t, jb.Err(), "queue b's job with the same ID is not cancelled")
}

// cancelPrio is cancelStore as priority storage.
type cancelPrio struct{ *cancelStore }

func (p cancelPrio) Enqueue(item any, prio int) bool { return p.push(item, prio) }
func (p cancelPrio) EnqueueOnce(item any, prio int, id string) bool {
	p.mu.Lock()
	delete(p.outcomes, id)
	p.mu.Unlock()
	p.cmu.Lock()
	delete(p.marked, id)
	p.cmu.Unlock()
	return p.push(item, prio)
}

// blockOnCancel is a worker function that reports its start and returns
// once its job's context is cancelled.
func blockOnCancel(started chan<- string) func(j Job[int]) {
	return func(j Job[int]) {
		started <- j.ID()
		<-j.Context().Done()
	}
}

func TestRemoteCancelReachesDeprecatedBinders(t *testing.T) {
	type adder = func(id string) bool
	binds := map[string]func(IWorkerBinder[int], *cancelStore) adder{
		"persistent": func(b IWorkerBinder[int], s *cancelStore) adder {
			q := b.WithPersistentQueue(s)
			return func(id string) bool { return q.Add(1, WithJobId(id)) }
		},
		"persistent priority": func(b IWorkerBinder[int], s *cancelStore) adder {
			q := b.WithPersistentPriorityQueue(cancelPrio{s})
			return func(id string) bool { return q.Add(1, 0, WithJobId(id)) }
		},
		"distributed": func(b IWorkerBinder[int], s *cancelStore) adder {
			b.WithDistributedQueue(s)
			return func(id string) bool { return NewDistributedQueue[int](s).Add(1, WithJobId(id)) }
		},
	}
	for name, bind := range binds {
		t.Run(name, func(t *testing.T) {
			s := newCancelStore()
			started := make(chan string, 1)
			w := NewWorker(blockOnCancel(started))
			defer w.Stop()
			add := bind(w, s)

			require.True(t, add("d"))
			<-started
			require.NoError(t, OpenJob(s, "d").Cancel())
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			assert.NoError(t, OpenJob(s, "d").WaitContext(ctx), "the running job saw the cancel")
		})
	}
}

func TestCancelThenPanic(t *testing.T) {
	s := newCancelStore()
	started := make(chan struct{})
	w := NewWorker(func(j Job[int]) {
		close(started)
		<-j.Context().Done()
		panic("after cancel")
	})
	defer w.Stop()
	w.WithQueue(s)

	j, _ := NewQueue[int](s).Add(1)
	<-started
	require.NoError(t, j.Cancel())
	j.Wait()
	assert.ErrorIs(t, <-w.Errs(), ErrJobCancelled)
	assert.ErrorIs(t, OpenErrJob(s, j.ID()).Err(), ErrJobCancelled, "storage finished it cancelled")
}

func TestRemoteCancelThenReAdd(t *testing.T) {
	s := newCancelStore()
	started := make(chan int, 2)
	release := make(chan struct{})
	w := NewErrWorker(func(j Job[int]) error {
		started <- j.Data()
		select {
		case <-j.Context().Done():
			return j.Context().Err()
		case <-release:
			return nil
		}
	})
	defer w.Stop()
	w.WithQueue(s)
	p := NewErrQueue[int](s)

	old, _ := p.Add(1, WithJobId("x"))
	<-started
	require.NoError(t, old.Cancel())
	require.ErrorIs(t, old.Err(), ErrJobCancelled)

	// the same ID again: the re-add clears the mark, so it runs
	readded, _ := p.Add(2, WithJobId("x"))
	assert.Equal(t, 2, <-started)
	assert.ErrorIs(t, old.Cancel(), ErrJobAlreadyClosed, "the old handle does not cancel the new job")
	close(release)
	assert.NoError(t, readded.Err())
}

func TestRemoteCancelEveryRunOfAnID(t *testing.T) {
	s := newCancelStore()
	started, stopped := make(chan string, 2), make(chan struct{}, 2)
	w := NewWorker(func(j Job[int]) {
		started <- j.ID()
		<-j.Context().Done()
		stopped <- struct{}{}
	}, WithConcurrency(2))
	defer w.Stop()
	w.WithQueue(s)

	// two live runs of one ID on one worker (a redelivery, a reused ID)
	p := NewQueue[int](s)
	p.Add(1, WithJobId("dup"))
	p.Add(2, WithJobId("dup"))
	<-started
	<-started
	require.NoError(t, OpenJob(s, "dup").Cancel())
	for range 2 {
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
			t.Fatal("a run of the ID missed the cancel")
		}
	}
}

// mapped is value-type storage whose type is comparable but whose value
// is not: its interface field holds a map.
type mapped struct {
	*cancelStore
	tag any
}

func TestUnhashableStorageGetsNoCancelWatch(t *testing.T) {
	plain, odd := newCancelStore(), mapped{newCancelStore(), map[string]int{}}
	started := make(chan string, 1)
	release := make(chan struct{})
	w := NewErrWorker(func(j Job[int]) error {
		started <- j.ID()
		select {
		case <-j.Context().Done():
			return j.Context().Err()
		case <-release:
			return nil
		}
	})
	defer w.Stop()
	w.WithQueue(plain) // turns the watch on
	q := w.WithQueue(odd)

	j, ok := q.Add(1)
	require.True(t, ok)
	<-started // tracking it did not panic
	require.NoError(t, OpenErrJob(odd, j.ID()).Cancel(), "requested")
	close(release)
	assert.NoError(t, j.Err(), "not reached: ran to the end")
}

func TestCancelWatchSkipsInMemoryJobs(t *testing.T) {
	w := NewWorker(func(Job[int]) {})
	defer w.Stop()
	w.WithQueue(newCancelStore()) // the watch is on
	j, _ := w.BindQueue().Add(1)  // no storage: not tracked
	j.Wait()
	assert.Equal(t, "Closed", j.Status())
}

// unsubscribed is cancelStore without Subscribe, so nothing but the
// cancel hook refers to the worker.
type unsubscribed struct{ *cancelStore }

func (unsubscribed) Subscribe() {}

func TestCancelHookDropsCollectedWorker(t *testing.T) {
	s := newCancelStore()
	func() {
		w := NewWorker(func(Job[int]) {})
		w.WithQueue(unsubscribed{s})
		require.NoError(t, w.Stop())
	}()
	require.Equal(t, 1, s.numHooks())

	for range 10 {
		runtime.GC()
		require.NoError(t, s.CancelJob("any"))
		if s.numHooks() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Skip("worker not collected yet; GC timing")
}

// cancelOnly can cancel by ID but reports no outcomes.
type cancelOnly struct{ *cancelStore }

func (cancelOnly) WatchOutcome() {}

func TestCancelOnlyStorageGetsAnID(t *testing.T) {
	j, ok := NewErrQueue[int](cancelOnly{newCancelStore()}).Add(1)
	require.True(t, ok)
	assert.NotEmpty(t, j.ID(), "an ID to cancel by")
	assert.ErrorIs(t, j.Err(), ErrDetached)
	assert.NoError(t, j.Cancel(), "a detached handle still cancels by ID")

	// ended on storage, unseen by this detached handle: storage says so
	s := newCancelStore()
	s.outcomes["e"] = JobOutcome{ID: "e", State: JobCompleted}
	assert.ErrorIs(t, watchRemote[struct{}](cancelOnly{s}, "e", nil).Cancel(), ErrJobAlreadyClosed)
}

// failingCancel can't reach its backend.
type failingCancel struct{ *sharedStore }

func (failingCancel) CancelJob(string) error { return errors.New("down") }

func TestRemoteCancelError(t *testing.T) {
	j, _ := NewQueue[int](failingCancel{newSharedStore()}).Add(1)
	assert.EqualError(t, j.Cancel(), "down")
}

func TestCancelEveryJobKind(t *testing.T) {
	queued := func(m interface{ markQueued() }) { m.markQueued() }

	j := newJob(1, jobConfigs{})
	queued(j)
	require.NoError(t, j.Cancel())
	assert.ErrorIs(t, j.Cancel(), ErrJobAlreadyClosed)

	rj := newResultJob[int, int](1, jobConfigs{})
	queued(rj)
	require.NoError(t, rj.Cancel())
	_, err := rj.Result()
	assert.ErrorIs(t, err, ErrJobCancelled)

	g := newGroupJob[int](1).newJob(1, jobConfigs{})
	queued(g)
	require.NoError(t, g.Cancel())
	g.Wait()

	eg := newErrorGroupJob[int](1)
	em := eg.newJob(1, jobConfigs{})
	queued(em)
	require.NoError(t, em.Cancel())
	for range eg.Errs() { // closed by the last member's release
	}

	rg := newResultGroupJob[int, int](1)
	rm := rg.newJob(1, jobConfigs{})
	queued(rm)
	require.NoError(t, rm.Cancel())
	for range rg.Results() {
	}
}

func TestCancelWaitingWorkflowTask(t *testing.T) {
	j := newErrorJob(1, jobConfigs{Id: "wf:w1:a"})
	j.setTask(taskTag{wf: "w1", task: "a"}) // no store: nothing to report to
	j.markQueued()
	require.NoError(t, j.Cancel())
	assert.ErrorIs(t, j.Err(), ErrJobCancelled)

	end := j.ackRef.Load().ext.end
	require.NotNil(t, end)
	assert.Equal(t, OutcomeCancelled, end.Outcome)
}

func TestEndRunWithNoRuns(t *testing.T) {
	j := newJob(1, jobConfigs{})
	if liveRuns.Load() == 0 {
		j.endRun() // nothing to drop
	}
	assert.False(t, j.cancelRequested())
}

// panicky panics when it is encoded.
type panicky struct{}

func (panicky) MarshalJSON() ([]byte, error) { panic("encode") }

func TestResultMarshalPanicFailsJob(t *testing.T) {
	s := newSharedStore()
	w := NewResultWorker(func(Job[int]) (panicky, error) { return panicky{}, nil })
	defer w.Stop()

	j, _ := w.WithQueue(s).Add(1)
	_, err := j.Result()
	var re *RemoteError
	require.ErrorAs(t, err, &re, "the worker survived and failed the job")
	assert.True(t, strings.Contains(re.Reason, "encode"), re.Reason)

	// a workflow task: the task fails
	tj := newResultJob[int, panicky](1, jobConfigs{Id: "wf:w1:a"})
	tj.setTask(taskTag{wf: "w1", task: "a"})
	tj.succeed()
	end := tj.ackRef.Load().ext.end
	require.NotNil(t, end)
	assert.Equal(t, OutcomeFailed, end.Outcome)
	assert.Contains(t, end.Reason, "encode")

	// in memory (no ack): nothing is encoded
	mj := newResultJob[int, panicky](1, jobConfigs{})
	mj.succeed()
	assert.Nil(t, mj.ackRef.Load())
}
