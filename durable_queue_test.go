package varmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sharedStore is shared storage in one process: FIFO or priority items as
// JSON, ack IDs, outcome watches and kept results, like redisq.
type sharedStore struct {
	mu       sync.Mutex
	items    []sharedItem
	pending  map[string]string // ack ID -> job ID
	outcomes map[string]JobOutcome
	watches  map[string][]func(JobOutcome)
	subs     []func(string)
	nextAck  int
}

type sharedItem struct {
	val  []byte
	prio int
}

func newSharedStore() *sharedStore {
	return &sharedStore{pending: map[string]string{}, outcomes: map[string]JobOutcome{}, watches: map[string][]func(JobOutcome){}}
}

func (s *sharedStore) push(item any, prio int) bool {
	b, ok := item.([]byte)
	if !ok {
		return false
	}
	s.mu.Lock()
	s.items = append(s.items, sharedItem{b, prio})
	sort.SliceStable(s.items, func(i, j int) bool { return s.items[i].prio < s.items[j].prio })
	subs := append([]func(string){}, s.subs...)
	s.mu.Unlock()
	for _, fn := range subs {
		fn("enqueued")
	}
	return true
}

func (s *sharedStore) Enqueue(item any) bool               { return s.push(item, 0) }
func (s *sharedStore) EnqueueOnce(item any, _ string) bool { return s.push(item, 0) }
func (s *sharedStore) Subscribe(fn func(string)) {
	s.mu.Lock()
	s.subs = append(s.subs, fn)
	s.mu.Unlock()
}
func (s *sharedStore) Dequeue() (any, bool)          { v, ok, _ := s.DequeueWithAckId(); return v, ok }
func (s *sharedStore) Values() []any                 { return nil }
func (s *sharedStore) Purge()                        { s.mu.Lock(); s.items = nil; s.mu.Unlock() }
func (s *sharedStore) Close() error                  { return nil }
func (s *sharedStore) Len() int                      { s.mu.Lock(); defer s.mu.Unlock(); return len(s.items) }
func (s *sharedStore) Acknowledge(ackID string) bool { return s.finish(ackID, JobCompleted, "", nil) }
func (s *sharedStore) Reject(ackID, reason string) bool {
	return s.finish(ackID, JobFailed, reason, nil)
}
func (s *sharedStore) AcknowledgeResult(a string, r []byte) bool {
	return s.finish(a, JobCompleted, "", r)
}

func (s *sharedStore) DequeueWithAckId() (any, bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) == 0 {
		return nil, false, ""
	}
	it := s.items[0]
	s.items = s.items[1:]
	var env struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(it.val, &env)
	s.nextAck++
	ack := fmt.Sprint("ack-", s.nextAck)
	s.pending[ack] = env.ID
	return it.val, true, ack
}

func (s *sharedStore) finish(ackID string, st JobState, reason string, result []byte) bool {
	s.mu.Lock()
	id, ok := s.pending[ackID]
	delete(s.pending, ackID)
	o := JobOutcome{ID: id, State: st, Error: reason, Result: result}
	var fns []func(JobOutcome)
	if ok && id != "" {
		s.outcomes[id] = o
		fns = s.watches[id]
		delete(s.watches, id)
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(o)
	}
	return ok
}

func (s *sharedStore) WatchOutcome(id string, fn func(JobOutcome)) func() {
	s.mu.Lock()
	if o, ended := s.outcomes[id]; ended {
		s.mu.Unlock()
		fn(o)
		return func() {}
	}
	s.watches[id] = append(s.watches[id], fn)
	n := len(s.watches[id]) - 1
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if w := s.watches[id]; n < len(w) {
			w[n] = func(JobOutcome) {}
		}
	}
}

// sharedPrio is sharedStore as priority storage.
type sharedPrio struct{ *sharedStore }

func (p sharedPrio) Enqueue(item any, prio int) bool               { return p.push(item, prio) }
func (p sharedPrio) EnqueueOnce(item any, prio int, _ string) bool { return p.push(item, prio) }

// resultless hides AcknowledgeResult.
type resultless struct{ *sharedStore }

func (resultless) AcknowledgeResult() {}

var (
	_ IPersistentQueue            = (*sharedStore)(nil)
	_ ISubscribable               = (*sharedStore)(nil)
	_ OutcomeWatcher              = (*sharedStore)(nil)
	_ ResultAcknowledgeable       = (*sharedStore)(nil)
	_ EnqueuedResultJob[int]      = (*remoteJob[int])(nil)
	_ EnqueuedJob                 = (*job[int])(nil) // WaitContext in memory too
	_ EnqueuedResultGroupJob[int] = (*remoteGroup[int])(nil)
	_ EnqueuedErrGroupJob         = (*remoteGroup[int])(nil)
)

func TestWithQueueSharedWorker(t *testing.T) {
	s := newSharedStore()
	ran := make(chan int, 1)
	w := NewWorker(func(j Job[int]) { ran <- j.Data() })
	defer w.Stop()

	q := w.WithQueue(s)
	j, ok := q.Add(7)
	require.True(t, ok)
	assert.NotEmpty(t, j.ID(), "an ID is generated to match the outcome")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, j.WaitContext(ctx))
	assert.Equal(t, 7, <-ran)
	assert.Equal(t, "Closed", j.Status())
}

func TestWithQueueSharedErrWorker(t *testing.T) {
	s := newSharedStore()
	w := NewErrWorker(func(j Job[string]) error {
		if j.Data() == "bad" {
			return errors.New("bad data")
		}
		return nil
	})
	defer w.Stop()

	q := w.WithQueue(s)
	good, _ := q.Add("ok")
	bad, _ := q.Add("bad")
	assert.NoError(t, good.Err())

	var re *RemoteError
	require.ErrorAs(t, bad.Err(), &re)
	assert.Equal(t, "bad data", re.Reason)
}

func TestWithQueueSharedResultWorker(t *testing.T) {
	type out struct{ N int64 }
	s := newSharedStore()
	w := NewResultWorker(func(j Job[int64]) (out, error) { return out{j.Data() * 2}, nil })
	defer w.Stop()

	q := w.WithQueue(s)
	j, ok := q.Add(1<<53 + 1) // not a float64: the JSON is kept, not re-encoded
	require.True(t, ok)
	r, err := j.Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1<<54+2), r.N)
}

func TestWithPriorityQueueSharedGroup(t *testing.T) {
	s := newSharedStore()
	w := NewResultWorker(func(j Job[int]) (int, error) {
		if j.Data() < 0 {
			return 0, errors.New("neg")
		}
		return j.Data() + 1, nil
	})
	defer w.Stop()

	g := w.WithPriorityQueue(sharedPrio{s}).AddAll([]Item[int]{{Data: 1, Priority: 2}, {Data: -1, Priority: 1}, {ID: "x", Data: 5}})
	g.Wait()
	assert.Equal(t, 0, g.NumPending())

	got := map[int]bool{}
	fails := 0
	for r := range g.Results() { // closes when the group ended
		if r.Err != nil {
			fails++
			continue
		}
		got[r.Data] = true
	}
	assert.Equal(t, 1, fails)
	assert.Equal(t, map[int]bool{2: true, 6: true}, got)
}

func TestResultTooLargeHasNoResult(t *testing.T) {
	s := newSharedStore()
	w := NewResultWorker(func(Job[int]) (string, error) { return string(make([]byte, remoteResultLimit)), nil })
	defer w.Stop()

	j, _ := w.WithQueue(s).Add(1)
	_, err := j.Result()
	assert.ErrorIs(t, err, ErrNoResult)
	assert.NoError(t, j.(EnqueuedErrJob).Err(), "the job still completed")
}

func TestStorageWithoutResultAck(t *testing.T) {
	s := resultless{newSharedStore()}
	w := NewResultWorker(func(Job[int]) (int, error) { return 3, nil })
	defer w.Stop()

	j, _ := w.WithQueue(s).Add(1)
	_, err := j.Result()
	assert.ErrorIs(t, err, ErrNoResult)
}

func TestProducerAndOpenJob(t *testing.T) {
	s := newSharedStore()
	p := NewResultQueue[int, int](s)
	j, ok := p.Add(4, WithJobId("job-4"))
	require.True(t, ok)
	assert.Equal(t, "job-4", j.ID())

	// consumer, as if in another process
	w := NewResultWorker(func(j Job[int]) (int, error) { return j.Data() * 10, nil })
	defer w.Stop()
	w.WithQueue(s)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, j.WaitContext(ctx))
	got, err := j.Result()
	require.NoError(t, err)
	assert.Equal(t, 40, got)

	// another waiter, by ID, after the end: typed result
	r, err := OpenResultJob[int](s, "job-4").Result()
	require.NoError(t, err)
	assert.Equal(t, 40, r)

	// unknown job: waits until ctx ends
	short, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	o := OpenJob(s, "nope")
	defer o.Close()
	assert.ErrorIs(t, o.WaitContext(short), context.DeadlineExceeded)

	// an ErrWorker-style wait by ID: Err directly
	assert.NoError(t, OpenErrJob(s, "job-4").Err())
}

func TestDetachedWithoutWatcher(t *testing.T) {
	s := newSharedStore()
	type plain struct{ IPersistentQueue } // no OutcomeWatcher
	p := NewErrQueue[int](plain{s})
	j, ok := p.Add(1)
	require.True(t, ok)
	assert.Empty(t, j.ID(), "no watcher: no generated ID")
	assert.ErrorIs(t, j.Err(), ErrDetached)
}

func TestRemoteJobCloseAndContext(t *testing.T) {
	s := newSharedStore()
	j, _ := NewErrQueue[int](s).Add(1) // no consumer

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, j.WaitContext(ctx), context.DeadlineExceeded)

	require.NoError(t, j.Close())
	assert.ErrorIs(t, j.Err(), ErrDetached)
	assert.True(t, j.IsClosed())
}

func TestPurgeDetachesHandles(t *testing.T) {
	s := newSharedStore()
	w := NewErrWorker(func(Job[int]) error { return nil })
	q := w.WithQueue(s)
	w.Pause() // nothing runs: the job stays queued
	defer w.Stop()

	j, ok := q.Add(1)
	require.True(t, ok)
	q.Purge()
	assert.ErrorIs(t, j.Err(), ErrDetached)
}

// refusing refuses every AcknowledgeResult, as Redis's cjson refuses JSON
// nested past 1000 levels.
type refusing struct{ *sharedStore }

func (refusing) AcknowledgeResult(string, []byte) bool { return false }

func TestRefusedResultStillCompletes(t *testing.T) {
	s := refusing{newSharedStore()}
	w := NewResultWorker(func(Job[int]) (int, error) { return 1, nil })
	defer w.Stop()

	j, _ := w.WithQueue(s).Add(1)
	_, err := j.Result()
	assert.ErrorIs(t, err, ErrNoResult)
}

func TestGivenIDWatchesAfterPush(t *testing.T) {
	s := newSharedStore()
	s.outcomes["x"] = JobOutcome{ID: "x", State: JobFailed, Error: "old run"}
	clearing := &clearOnPush{s}
	p := NewErrQueue[int](clearing)
	j, _ := p.Add(1, WithJobId("x"))

	w := NewErrWorker(func(Job[int]) error { return nil })
	defer w.Stop()
	w.WithQueue(clearing)
	assert.NoError(t, j.Err(), "not the old run's failure")
}

// clearOnPush drops an id's old outcome on a push with it, as spec 6.13 asks.
type clearOnPush struct{ *sharedStore }

func (c *clearOnPush) EnqueueOnce(item any, id string) bool {
	c.mu.Lock()
	delete(c.outcomes, id)
	c.mu.Unlock()
	return c.push(item, 0)
}

func TestResultProducer(t *testing.T) {
	s := newSharedStore()
	w := NewResultWorker(func(j Job[int]) (string, error) { return fmt.Sprint("r-", j.Data()), nil })
	defer w.Stop()
	w.WithQueue(s)

	j, ok := NewResultQueue[int, string](s).Add(3)
	require.True(t, ok)
	r, err := j.Result()
	require.NoError(t, err)
	assert.Equal(t, "r-3", r)

	pj, ok := NewResultPriorityQueue[int, string](sharedPrio{newSharedStore()}).Add(1, 0)
	require.True(t, ok)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, pj.WaitContext(ctx), context.DeadlineExceeded, "no consumer: still waiting")
}

func TestPlainProducerWaits(t *testing.T) {
	s := newSharedStore()
	w := NewWorker(func(Job[int]) {})
	defer w.Stop()
	w.WithQueue(s)

	j, ok := NewQueue[int](s).Add(1) // EnqueuedJob: Wait only, as NewWorker's
	require.True(t, ok)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, j.WaitContext(ctx))

	_, ok = NewErrPriorityQueue[int](sharedPrio{s}).Add(2, 0)
	assert.True(t, ok)
}

func TestInMemoryWaitContext(t *testing.T) {
	release := make(chan struct{})
	w := NewWorker(func(Job[int]) { <-release })
	defer w.Stop()
	j, _ := w.BindQueue().Add(1)

	short, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	assert.ErrorIs(t, j.WaitContext(short), context.DeadlineExceeded)

	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.NoError(t, j.WaitContext(ctx))
}

func TestCloseDetachesHandles(t *testing.T) {
	q := NewErrQueue[int](newSharedStore())
	j, ok := q.Add(1) // no consumer: it would wait for good
	require.True(t, ok)
	require.NoError(t, q.Close())
	assert.ErrorIs(t, j.Err(), ErrDetached)
}

func TestResultGroupKeepsErrNoResult(t *testing.T) {
	s := resultless{newSharedStore()} // storage keeps no results
	w := NewResultWorker(func(Job[int]) (int, error) { return 1, nil })
	defer w.Stop()

	g := w.WithQueue(s).AddAll([]Item[int]{{Data: 1}, {Data: 2}})
	n := 0
	for r := range g.Results() {
		n++
		assert.ErrorIs(t, r.Err, ErrNoResult, "the result is gone, not zero")
	}
	assert.Equal(t, 2, n)

}
