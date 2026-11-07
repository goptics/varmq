package varmq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goptics/varmq/internal/pool"
	"github.com/goptics/varmq/internal/queues"
	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stays fails if cond holds at any point within d.
func stays(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	assert.Never(t, cond, 50*time.Millisecond, time.Millisecond, msg)
}

// A paused queue runs nothing; adds land; the handle counts its jobs, the
// worker does not, so Wait returns; Resume runs them all.
func TestQueuePause(t *testing.T) {
	var ran atomic.Int64
	w := NewWorker(func(Job[int]) { ran.Add(1) }, 2)
	defer w.Stop()

	fifo, prio := w.BindQueue(), w.BindPriorityQueue()
	pq := w.WithPersistentQueue(mocks.NewMockPersistentQueue()) // acknowledging storage: the ack path
	for name, c := range map[string]struct {
		IExternalBaseQueue
		add func(int)
	}{
		"fifo":       {fifo, func(i int) { fifo.Add(i) }},
		"priority":   {prio, func(i int) { prio.Add(i, i) }},
		"persistent": {pq, func(i int) { pq.Add(i) }},
	} {
		q := c.IExternalBaseQueue
		t.Run(name, func(t *testing.T) {
			ran.Store(0)
			require.NoError(t, q.Pause())
			assert.True(t, q.IsPaused())
			for i := range 5 {
				c.add(i)
			}

			stays(t, func() bool { return ran.Load() > 0 }, "nothing runs while paused")
			assert.Equal(t, 5, q.Len(), "the handle counts paused jobs")
			assert.Zero(t, w.NumPending(), "the worker does not")
			w.Wait() // returns: paused jobs are not the worker's

			require.NoError(t, q.Resume())
			require.Eventually(t, func() bool { return ran.Load() == 5 }, time.Second, time.Millisecond)
		})
	}
}

// A Resume wakes every worker bound to the storage, through any handle.
func TestQueueResumeWakesEveryWorker(t *testing.T) {
	storage := queues.NewQueue[any]()
	var ran atomic.Int64
	var handles []Queue[int]
	for range 3 {
		w := NewWorker(func(Job[int]) { ran.Add(1) })
		defer w.Stop()
		handles = append(handles, w.WithQueue(storage))
	}

	require.NoError(t, handles[0].Pause())
	for i, h := range handles {
		h.Add(i)
	}
	stays(t, func() bool { return ran.Load() > 0 }, "paused for every worker")

	require.NoError(t, handles[2].Resume())
	require.Eventually(t, func() bool { return ran.Load() == 3 }, time.Second, time.Millisecond)
}

// The worker pause and the queue pause are separate: a job runs only when
// neither is paused, and resuming one does not resume the other.
func TestWorkerAndQueuePause(t *testing.T) {
	var ran atomic.Int64
	w := NewWorker(func(Job[int]) { ran.Add(1) })
	defer w.Stop()
	q := w.BindQueue()

	require.NoError(t, w.Pause())
	require.NoError(t, q.Pause())
	q.Add(1)

	require.NoError(t, w.Resume())
	stays(t, func() bool { return ran.Load() > 0 }, "queue still paused")

	require.NoError(t, w.Pause())
	require.NoError(t, q.Resume())
	stays(t, func() bool { return ran.Load() > 0 }, "worker still paused")

	require.NoError(t, w.Resume())
	require.Eventually(t, func() bool { return ran.Load() == 1 }, time.Second, time.Millisecond)
}

// A batch the worker took before the Pause is cut at the next job: the rest
// waits, out of the worker's pending, until the Resume.
func TestQueuePauseParksBatchTail(t *testing.T) {
	release := make(chan struct{})
	var ran atomic.Int64
	w := NewWorker(func(j Job[int]) {
		if j.Data() == 0 {
			<-release
		}
		ran.Add(1)
	}, 1)
	defer w.Stop()
	q := w.BindQueue()

	require.NoError(t, w.Pause())
	for i := range 10 {
		q.Add(i)
	}
	require.NoError(t, w.Resume()) // concurrency 1: one batch of all 10
	require.Eventually(t, func() bool { return q.Len() == 0 }, time.Second, time.Millisecond, "taken in one batch")

	require.NoError(t, q.Pause())
	close(release)
	require.Eventually(t, func() bool { return ran.Load() == 1 }, time.Second, time.Millisecond)
	stays(t, func() bool { return ran.Load() > 1 }, "the rest waits")
	assert.Zero(t, w.NumPending(), "held jobs are not pending")
	w.Wait()

	require.NoError(t, q.Resume())
	require.Eventually(t, func() bool { return ran.Load() == 10 }, time.Second, time.Millisecond)
}

// staleStorage is paused by another process: its Paused cache says running
// until a Dequeue finds the flag, as QueuePauser requires.
type staleStorage struct {
	*queues.Queue[any]
	remote   atomic.Bool
	cache    atomic.Bool
	dequeues atomic.Int64
}

func (s *staleStorage) Pause() error  { s.remote.Store(true); s.cache.Store(true); return nil }
func (s *staleStorage) Resume() error { s.remote.Store(false); s.cache.Store(false); return nil }
func (s *staleStorage) Paused() bool  { return s.cache.Load() }
func (s *staleStorage) Dequeue() (any, bool) {
	s.dequeues.Add(1)
	if s.remote.Load() {
		s.cache.Store(true)
		return nil, false
	}
	return s.Queue.Dequeue()
}

// A stale cache costs one empty dequeue, never a spin.
func TestQueuePauseStaleCache(t *testing.T) {
	s := &staleStorage{Queue: queues.NewQueue[any]()}
	var ran, errs atomic.Int64
	w := NewWorker(func(Job[int]) { ran.Add(1) }, WithErrHandler(func(Worker, error) { errs.Add(1) }))
	defer w.Stop()
	q := w.WithQueue(s)

	s.remote.Store(true) // paused elsewhere; this process has not heard
	q.Add(1)
	stays(t, func() bool { return ran.Load() > 0 }, "the storage refuses")
	assert.LessOrEqual(t, s.dequeues.Load(), int64(2), "no spin")
	assert.True(t, q.IsPaused(), "the refused dequeue set the cache")

	require.NoError(t, q.Resume()) // no PauseNotifier: the next add wakes it
	q.Add(2)
	require.Eventually(t, func() bool { return ran.Load() == 2 }, time.Second, time.Millisecond)
	assert.Zero(t, errs.Load(), "a refused dequeue is not an error")
}

// Storage that cannot pause says so.
func TestQueuePauseUnsupported(t *testing.T) {
	w := NewWorker(func(Job[int]) {})
	defer w.Stop()
	// Embedding an interface hides the mocks' in-memory pause.
	q := w.WithPersistentQueue(struct{ IPersistentQueue }{mocks.NewMockPersistentQueue()})
	assert.ErrorIs(t, q.Pause(), ErrPauseUnsupported)
	assert.ErrorIs(t, q.Resume(), ErrPauseUnsupported)
	assert.False(t, q.IsPaused())

	dq := NewDistributedQueue[int](struct{ IDistributedQueue }{mocks.NewMockDistributedQueue()})
	assert.ErrorIs(t, dq.Pause(), ErrPauseUnsupported)
	dpq := NewDistributedPriorityQueue[int](struct{ IDistributedPriorityQueue }{mocks.NewMockDistributedPriorityQueue()})
	assert.ErrorIs(t, dpq.Resume(), ErrPauseUnsupported)

	pausable := NewDistributedQueue[int](mocks.NewMockDistributedQueue())
	require.NoError(t, pausable.Pause(), "a distributed handle forwards to its storage")
	assert.True(t, pausable.IsPaused())
	require.NoError(t, pausable.Resume())
	pprio := NewDistributedPriorityQueue[int](mocks.NewMockDistributedPriorityQueue())
	require.NoError(t, pprio.Pause())
	assert.True(t, pprio.IsPaused())
	require.NoError(t, pprio.Resume())
	assert.False(t, pprio.IsPaused())
}

// Unbinding a queue cancels its Resume hook.
func TestQueuePauseHookCancelled(t *testing.T) {
	w := NewWorker(func(Job[int]) {}).(*workerBinder[int])
	defer w.Stop()
	storage := queues.NewQueue[any]()
	src := w.bind(storage, 0)
	_, ok := w.pauseHooks.Load(src)
	require.True(t, ok)

	w.removeSource(src)
	_, ok = w.pauseHooks.Load(src)
	assert.False(t, ok)
	storage.Pause() // the cancelled hook is gone: Resume calls nothing
	assert.NoError(t, storage.Resume())
}

// plainInspector is a listed queue that cannot pause.
type plainInspector struct{}

func (plainInspector) QueueID() string { return "custom:plain" }
func (plainInspector) QueueStats(context.Context) (QueueStats, error) {
	return QueueStats{}, nil
}
func (plainInspector) Jobs(context.Context, JobState, int, int) ([]JobRecord, error) {
	return nil, nil
}

// failingPauser is a listed queue whose storage cannot store the flag.
type failingPauser struct{ plainInspector }

func (failingPauser) QueueID() string { return "custom:failing" }
func (failingPauser) Pause() error    { return errors.New("redis down") }
func (failingPauser) Resume() error   { return errors.New("redis down") }
func (failingPauser) Paused() bool    { return false }

func TestMuxQueueActions(t *testing.T) {
	w := NewWorker(func(Job[int]) {}, WithName("pause-rest"))
	defer w.Stop()
	m := memOf(t, w.BindQueue())
	queueRegistry.Store("custom:plain", plainInspector{})
	queueRegistry.Store("custom:failing", failingPauser{})
	defer queueRegistry.Delete("custom:plain")
	defer queueRegistry.Delete("custom:failing")

	server := NewServerMux()
	patch := func(id, action string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest("PATCH", "/queues/"+id+"/actions/"+action, nil))
		return rr
	}

	rr := patch(m.QueueID(), "pause")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `"paused":true`)
	assert.Contains(t, rr.Body.String(), `"paused_at":`)
	s, _ := m.QueueStats(context.Background())
	assert.True(t, s.Paused)
	assert.NotZero(t, s.PausedAt)

	rr = patch(m.QueueID(), "resume")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"paused":false`)
	assert.NotContains(t, rr.Body.String(), "paused_at")

	assert.Equal(t, http.StatusBadRequest, patch(m.QueueID(), "stop").Code)
	assert.Equal(t, http.StatusNotFound, patch("nope", "pause").Code)
	assert.Equal(t, http.StatusNotImplemented, patch("custom:plain", "pause").Code)
	assert.Equal(t, http.StatusServiceUnavailable, patch("custom:failing", "resume").Code)
}

// Pause and Resume racing workers and adds stay consistent (run with -race).
func TestQueuePauseRace(t *testing.T) {
	var ran, errs atomic.Int64
	w := NewWorker(func(Job[int]) { ran.Add(1) }, 4, WithErrHandler(func(Worker, error) { errs.Add(1) }))
	defer w.Stop()
	q := w.BindQueue()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 2000 {
			q.Add(i)
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			q.Pause()
			q.Resume()
		}
	}()
	wg.Wait()
	require.NoError(t, q.Resume())
	require.Eventually(t, func() bool { return ran.Load() == 2000 }, 5*time.Second, time.Millisecond)
	assert.Zero(t, errs.Load(), "a Pause between Len and Dequeue is not an error")
}

// unpark skips batches of a paused queue: it hands out a later unheld one,
// and nothing while only held ones remain.
func TestUnparkSkipsHeld(t *testing.T) {
	w := newWorker(func(iJob[int]) {}, WithAutoRun(false))
	gen := &atomic.Uint64{}
	held := queues.NewQueue[any]()
	require.NoError(t, held.Pause())
	w.park([]iJob[int]{newJob(1, jobConfigs{})}, pool.Stamp{Gen: gen, Seq: 1, Hold: held})
	w.park([]iJob[int]{newJob(2, jobConfigs{})}, pool.Stamp{Gen: gen, Seq: 2})

	buf := make([]iJob[int], 4)
	n, _ := w.unpark(buf)
	require.Equal(t, 1, n)
	assert.Equal(t, 2, buf[0].Data(), "the unheld batch behind the held one")
	assert.Equal(t, 0, w.unheld())
	n, _ = w.unpark(buf)
	assert.Zero(t, n, "only a held batch left")

	require.NoError(t, held.Resume())
	n, _ = w.unpark(buf)
	require.Equal(t, 1, n)
	assert.Equal(t, 1, buf[0].Data())
}

// pausableBatchQueue is batch-acknowledging storage with a pause: the credit
// path (a worker's sole IBatchAcknowledgeable source).
type pausableBatchQueue struct {
	*batchTestQueue
	p *queues.Queue[any] // only its pause
}

func (q *pausableBatchQueue) Pause() error  { return q.p.Pause() }
func (q *pausableBatchQueue) Resume() error { return q.p.Resume() }
func (q *pausableBatchQueue) Paused() bool  { return q.p.Paused() }
func (q *pausableBatchQueue) OnPauseChange(fn func(bool)) func() {
	return q.p.OnPauseChange(fn)
}
func (q *pausableBatchQueue) DequeueBatchWithAckIds(items []any, ackIDs []string) (int, int, error) {
	if q.p.Paused() {
		return 0, 0, nil
	}
	return q.batchTestQueue.DequeueBatchWithAckIds(items, ackIDs)
}

// On the credit path a paused queue is not popped, and a Resume pokes the
// ready hint so every job runs.
func TestQueuePauseCreditPath(t *testing.T) {
	s := &pausableBatchQueue{batchTestQueue: &batchTestQueue{}, p: queues.NewQueue[any]()}
	var ran atomic.Int64
	w := NewWorker(func(Job[int]) { ran.Add(1) }, 4)
	defer w.Stop()
	q := w.WithPersistentQueue(s)
	_, credit := w.(*workerBinder[int]).manager().(*creditManager[iJob[int]])
	require.True(t, credit, "sole batch source: credit path")

	require.NoError(t, q.Pause())
	pops := s.pops.Load()
	for i := range 10 {
		q.Add(i)
	}
	stays(t, func() bool { return s.pops.Load() > pops }, "no pop while paused")

	require.NoError(t, q.Resume())
	require.Eventually(t, func() bool { return ran.Load() == 10 }, time.Second, time.Millisecond)
}

// countingPause counts the cancels of its pause hooks.
type countingPause struct {
	*queues.Queue[any]
	cancels atomic.Int64
}

func (q *countingPause) OnPauseChange(fn func(bool)) func() {
	c := q.Queue.OnPauseChange(fn)
	return func() { q.cancels.Add(1); c() }
}

// The hook of a worker collected without unbinding drops itself from the
// storage at the next Pause or Resume.
func TestPauseHookOfCollectedWorker(t *testing.T) {
	s := &countingPause{Queue: queues.NewQueue[any]()}
	func() {
		w := NewWorker(func(Job[int]) {})
		w.WithQueue(s)
		require.NoError(t, w.Stop())
	}()

	require.Eventually(t, func() bool {
		runtime.GC()
		require.NoError(t, s.Pause())
		require.NoError(t, s.Resume())
		return s.cancels.Load() == 1
	}, 5*time.Second, 10*time.Millisecond)
}
