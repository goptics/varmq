package varmq

import (
	"context"
	"encoding/json"
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
// that drops the job at pop, a notice to consumers, a cancelled finish.
type cancelStore struct {
	*sharedStore
	cmu    sync.Mutex
	marked map[string]bool
	hooks  []func(string)
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
	hooks := append([]func(string){}, c.hooks...)
	c.cmu.Unlock()
	for _, fn := range hooks {
		fn(id)
	}
	return nil
}

func (c *cancelStore) OnCancel(fn func(string)) func() {
	c.cmu.Lock()
	c.hooks = append(c.hooks, fn)
	c.cmu.Unlock()
	return func() {}
}

func (c *cancelStore) DequeueWithAckId() (any, bool, string) {
	for {
		v, ok, ack := c.sharedStore.DequeueWithAckId()
		if !ok {
			return v, ok, ack
		}
		var env struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(v.([]byte), &env)
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
