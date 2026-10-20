package varmq

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gauge records how many jobs run at once.
type gauge struct {
	cur, max atomic.Int64
}

func (g *gauge) enter() {
	c := g.cur.Add(1)
	for m := g.max.Load(); c > m && !g.max.CompareAndSwap(m, c); m = g.max.Load() {
	}
}

func (g *gauge) leave() { g.cur.Add(-1) }

// One pop takes a job for every free slot, never more.
func TestCreditPopsForFreeSlots(t *testing.T) {
	const n, conc = 400, 8
	q := &batchTestQueue{}
	for i := range n {
		q.push(jobBytes(t, fmt.Sprint(i)))
	}
	var g gauge
	var done atomic.Int64
	wb := NewWorker(func(Job[string]) {
		g.enter()
		time.Sleep(2 * time.Millisecond)
		g.leave()
		done.Add(1)
	}, WithConcurrency(conc))
	defer wb.Stop()
	wb.WithPersistentQueue(q)

	require.Eventually(t, func() bool { return done.Load() == n }, 10*time.Second, time.Millisecond)
	assert.EqualValues(t, conc, q.maxWant.Load(), "the first pop fills every slot")
	assert.Less(t, q.pops.Load(), int64(n), "some pops take several jobs")
	assert.LessOrEqual(t, g.max.Load(), int64(conc), "never more jobs than slots")
}

// The credit counts as processing while the pop is in flight, so Wait does
// not return between the pop taking a job and the job starting.
func TestCreditReservedDuringPop(t *testing.T) {
	q := &batchTestQueue{}
	var r recorder
	wb := NewWorker(r.run)
	defer wb.Stop()
	pq := wb.WithPersistentQueue(q)
	waitPops(t, q, 1)

	release := make(chan struct{})
	block := func() { <-release }
	q.duringPop.Store(&block)
	require.True(t, pq.Add("a"))
	require.Eventually(t, func() bool { return q.pops.Load() == 2 }, time.Second, time.Millisecond)

	waited := make(chan struct{})
	go func() { wb.Wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("Wait returned while the popped job had not run")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-waited
	assert.True(t, r.has("a"))
}

// A Pause while a pop is in flight lets the popped jobs run, then settles
// Paused, and pops nothing more.
func TestCreditPauseDuringPop(t *testing.T) {
	q := &batchTestQueue{}
	var r recorder
	wb := NewWorker(r.run, WithConcurrency(4))
	defer wb.Stop()
	w := internalWorker(wb)
	pq := wb.WithPersistentQueue(q)
	waitPops(t, q, 1)

	release := make(chan struct{})
	block := func() { <-release }
	q.duringPop.Store(&block)
	require.True(t, pq.Add("a"))
	require.Eventually(t, func() bool { return q.pops.Load() == 2 }, time.Second, time.Millisecond)

	require.NoError(t, wb.Pause())
	assert.Equal(t, pausing, w.status.Load(), "a popped job is in flight")
	close(release)
	wb.WaitUntilPaused()
	assert.True(t, r.has("a"), "the popped job ran")

	pops := q.pops.Load()
	require.True(t, pq.Add("b"))
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, pops, q.pops.Load(), "no pop while paused")
	assert.False(t, r.has("b"))
}

// After TunePool lowers concurrency below the busy count, no pop runs until
// jobs finish, and no more jobs run at once than the new limit.
func TestCreditTunePoolShrink(t *testing.T) {
	const n = 60
	q := &batchTestQueue{}
	for i := range n {
		q.push(jobBytes(t, fmt.Sprint(i)))
	}
	var g gauge
	var after gauge
	var shrunk atomic.Bool
	var done atomic.Int64
	wb := NewWorker(func(Job[string]) {
		late := shrunk.Load()
		g.enter()
		if late {
			after.enter()
		}
		time.Sleep(5 * time.Millisecond)
		if late {
			after.leave()
		}
		g.leave()
		done.Add(1)
	}, WithConcurrency(6))
	defer wb.Stop()
	wb.WithPersistentQueue(q)

	require.Eventually(t, func() bool { return g.cur.Load() == 6 }, time.Second, time.Millisecond)
	require.NoError(t, wb.TunePool(2))
	shrunk.Store(true)
	require.Eventually(t, func() bool { return done.Load() == n }, 10*time.Second, time.Millisecond)
	assert.LessOrEqual(t, after.max.Load(), int64(2), "jobs started after the shrink respect it")
}

// A source bound with a higher priority in the middle of a drain is served
// before the drain ends: a credit pass never loops past a signal, so the next
// one sees the new manager.
func TestCreditSecondSourceMidDrain(t *testing.T) {
	const n = 50
	q := &batchTestQueue{}
	for i := range n {
		q.push(jobBytes(t, fmt.Sprint(i)))
	}
	var mu sync.Mutex
	var order []string
	wb := NewWorker(func(j Job[string]) {
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		order = append(order, j.Data())
		mu.Unlock()
	})
	defer wb.Stop()
	wb.WithPersistentQueue(q)
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(order) >= 3 }, time.Second, time.Millisecond)

	mq := wb.BindQueue(WithQueuePriority(-1))
	_, ok := mq.Add("mem")
	require.True(t, ok)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, v := range order {
			if v == "mem" {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Less(t, len(order), n, "served before the batch source drained")
}
