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
	}, WithConcurrency(conc), WithAutoRun(false))
	defer wb.Stop()
	wb.WithPersistentQueue(q)
	require.NoError(t, wb.Start()) // after the bind: no signal reaches the loop before the batch source

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

// A TunePool down while a pop is in flight: the popped jobs that find no slot
// are parked and start one slot at a time, in order.
func TestCreditTunePoolDuringPop(t *testing.T) {
	const n = 40 // enough parked jobs that an unstable sort would reorder them
	q := &batchTestQueue{}
	var g gauge
	var mu sync.Mutex
	var order []string
	var tuned atomic.Bool
	var after gauge
	wb := NewWorker(func(j Job[string]) {
		late := tuned.Load()
		g.enter()
		if late {
			after.enter()
		}
		time.Sleep(200 * time.Microsecond)
		mu.Lock()
		order = append(order, j.Data())
		mu.Unlock()
		if late {
			after.leave()
		}
		g.leave()
	}, WithConcurrency(n))
	defer wb.Stop()
	pq := wb.WithPersistentQueue(q)
	waitPops(t, q, 1)

	block := func() {
		require.NoError(t, wb.TunePool(1))
		tuned.Store(true)
	}
	q.duringPop.Store(&block) // the pop has taken its items; the shrink lands before any starts
	var want []string
	for i := range n {
		q.push(jobBytes(t, fmt.Sprint(i)))
		want = append(want, fmt.Sprint(i))
	}
	require.True(t, pq.Add("x"))
	want = append(want, "x")
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(order) == n+1 }, 5*time.Second, time.Millisecond)
	assert.LessOrEqual(t, after.max.Load(), int64(1), "no job starts over the new limit")
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, want, order, "parked jobs keep their order")
}

// NumPending leaves out the credit a pass reserved but has not spent: a pop
// in flight does not show as pending jobs.
func TestCreditNumPendingDuringPop(t *testing.T) {
	q := &batchTestQueue{}
	wb := NewWorker(func(Job[string]) {}, WithConcurrency(8))
	defer wb.Stop()
	pq := wb.WithPersistentQueue(q)
	waitPops(t, q, 1)

	release := make(chan struct{})
	var during atomic.Int64
	block := func() { during.Store(int64(wb.NumPending())); <-release }
	q.duringPop.Store(&block)
	require.True(t, pq.Add("a"))
	require.Eventually(t, func() bool { return q.pops.Load() == 2 }, time.Second, time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	assert.LessOrEqual(t, int64(wb.NumPending()), int64(1), "not the whole credit of 8")
	close(release)
	wb.Wait()
	assert.LessOrEqual(t, during.Load(), int64(1))
}

// Pause and Resume in a loop while jobs arrive: once WaitUntilPaused
// returns, no job starts until Resume.
func TestCreditPauseStress(t *testing.T) {
	q := &batchTestQueue{}
	var started atomic.Int64
	wb := NewWorker(func(Job[string]) {
		started.Add(1)
		time.Sleep(20 * time.Microsecond)
	}, WithConcurrency(4))
	defer wb.Stop()
	pq := wb.WithPersistentQueue(q)
	waitPops(t, q, 1)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				pq.Add(fmt.Sprint(i))
			}
		}
	}()
	defer func() { close(stop); wg.Wait() }()

	for end := time.Now().Add(1500 * time.Millisecond); time.Now().Before(end); {
		require.NoError(t, wb.Pause())
		wb.WaitUntilPaused()
		s := started.Load()
		time.Sleep(200 * time.Microsecond)
		if got := started.Load(); got != s {
			t.Fatalf("%d jobs started while paused", got-s)
		}
		require.NoError(t, wb.Resume())
	}
}

// hookSrc runs a hook the next time dispatch reads its hint.
type hookSrc struct {
	*batchAckSource[string, iJob[string]]
	onReady atomic.Pointer[func()]
}

func (h *hookSrc) ready() int {
	if f := h.onReady.Swap(nil); f != nil {
		(*f)()
	}
	return h.batchAckSource.ready()
}

// A Pause that lands between a pass reading the hint and reserving its
// credit is seen by the re-check: the pass releases the credit and pops
// nothing.
func TestCreditPauseBeforeReserve(t *testing.T) {
	var r recorder
	wb := NewWorker(r.run)
	defer wb.Stop()
	w := internalWorker(wb)
	q := &batchTestQueue{}
	h := &hookSrc{batchAckSource: newBatchAckSource(ackQueue(q), q, w)}
	w.addSource(h, 0)
	w.notifyToPullNextJobs()
	waitPops(t, q, 1) // empty: the hint is dry

	q.push(jobBytes(t, "a"))
	h.poke()
	pause := func() { require.NoError(t, wb.Pause()) }
	h.onReady.Store(&pause)
	w.notifyToPullNextJobs()
	wb.WaitUntilPaused()
	time.Sleep(30 * time.Millisecond)
	assert.EqualValues(t, 1, q.pops.Load(), "no pop after the Pause")
	assert.False(t, r.has("a"))
	assert.Zero(t, w.curProcessing.Load(), "the credit was released")
}

// Above maxCredit free slots, a pass pops maxCredit and signals the loop,
// whose next pass fills the rest.
func TestCreditCap(t *testing.T) {
	const conc, n = 100, 200
	q := &batchTestQueue{}
	for i := range n {
		q.push(jobBytes(t, fmt.Sprint(i)))
	}
	var g gauge
	release := make(chan struct{})
	wb := NewWorker(func(Job[string]) { g.enter(); <-release; g.leave() }, WithConcurrency(conc), WithAutoRun(false))
	defer wb.Stop()
	defer close(release)
	wb.WithPersistentQueue(q)
	require.NoError(t, wb.Start())

	require.Eventually(t, func() bool { return g.cur.Load() == conc }, time.Second, time.Millisecond, "every slot fills")
	assert.EqualValues(t, maxCredit, q.maxWant.Load())
	assert.EqualValues(t, 2, q.pops.Load(), "64, then the other 36")
}

// A job ending while the hint says dry does not pop: each add pops once.
func TestCreditNoPopWhileDry(t *testing.T) {
	q := &batchTestQueue{}
	wb := NewWorker(func(Job[string]) {})
	defer wb.Stop()
	pq := wb.WithPersistentQueue(q)
	waitPops(t, q, 1)
	const n = 10
	for i := range n {
		require.True(t, pq.Add(fmt.Sprint(i)))
		wb.Wait()
		time.Sleep(5 * time.Millisecond) // the job's end signal is handled
	}
	assert.EqualValues(t, 1+n, q.pops.Load())
}

// Credit dispatch reuses idle pool goroutines: there are never more than
// concurrency of them.
func TestCreditReusesPool(t *testing.T) {
	const conc, n = 4, 400
	q := &batchTestQueue{}
	for i := range n {
		q.push(jobBytes(t, fmt.Sprint(i)))
	}
	var done atomic.Int64
	wb := NewWorker(func(Job[string]) { time.Sleep(100 * time.Microsecond); done.Add(1) }, WithConcurrency(conc))
	defer wb.Stop()
	wb.WithPersistentQueue(q)
	maxIdle := 0
	for done.Load() < n {
		maxIdle = max(maxIdle, wb.NumIdleWorkers())
		time.Sleep(time.Millisecond)
	}
	assert.LessOrEqual(t, maxIdle, conc)
}

// releaseWaiters, called with a processing count loaded before a credit pass
// reserved, puts the status back to Running: a Pause then waits for the
// pass's jobs instead of settling Paused under them.
func TestReleaseWaitersSeesLateReserve(t *testing.T) {
	w := newWorker(func(iJob[string]) {}, WithAutoRun(false))
	w.status.Store(running)
	w.curProcessing.Store(3) // a pass reserved after the finisher loaded 0
	w.mx.Lock()
	w.releaseWaiters(0)
	w.mx.Unlock()
	assert.Equal(t, running, w.status.Load())
}
