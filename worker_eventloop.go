package varmq

import (
	"context"
	"errors"
	"time"

	"github.com/goptics/varmq/internal/pool"
)

func (w *worker[T, JobType]) goEventLoop() {
	go func(ctx context.Context, signal <-chan struct{}) {
		for {
			select {
			case <-ctx.Done():
				w.status.Store(stopping)
				w.Wait()
				w.removeAllWorkers()
				w.mx.Lock()
				w.status.Store(stopped)
				w.waiters.Broadcast()
				w.scheduleRetentionCleanup()
				w.mx.Unlock()
				return
			case <-signal:
				// A lone batch source pops for every free slot at once; parked
				// jobs go first through the classic loop.
				if cm, ok := w.manager().(*creditManager[JobType]); ok && w.nParked.Load() == 0 {
					w.dispatchCredit(cm)
					continue
				}

				for w.IsActive() && w.busyWorkers.Load() < w.concurrency.Load() && w.pending(w.manager()) > 0 {
					cont, err := w.processNextJob()
					if err != nil {
						w.sendError(err)
					}
					if !cont {
						break
					}
				}
			}
		}
	}(w.ctx, w.eventLoopSignal)
}

// maxBatch caps how many jobs one wake-up hands to a worker.
const maxBatch = 32

// spawnNanos is the wall-time cost of one more pool goroutine: the spawn on
// an event loop sharing a few cores, plus the stack it allocates and the GC
// later scans.
//
// ponytail: a constant; measuring the real cost per worker would need the
// GC share, which no single goroutine can see.
const spawnNanos = 2_000

// worthSpawning reports whether one more goroutine would shorten the drain
// of the backlog by more than it costs. With alive goroutines each job in
// the backlog waits about pending*jobNanos/alive; one more cuts that by
// pending*jobNanos/alive^2. Cheap jobs stop the pool at a few dozen
// goroutines, ten-millisecond ones at tens of thousands.
func worthSpawning(pending int, jobNanos int64, alive int) bool {
	if alive == 0 || jobNanos == 0 {
		return true // nothing measured yet: fair share decides
	}

	return int64(pending)*jobNanos > spawnNanos*int64(alive)*int64(alive)
}

// batchSize is one worker's share when the backlog is split across shares
// workers: at least 1, at most maxBatch. planBatch decides what shares is.
func batchSize(pending, shares int) int {
	return max(1, min(pending/max(shares, 1), maxBatch))
}

// handoffGap is the average spacing between hand-offs that keeps the event
// loop out of the way: at about 1 µs per hand-off (channel send plus
// goroutine wake), one every 32 µs is ~3% of its time. Workers finishing jobs
// faster than that get batches just big enough to bring the rate back under it.
// 32 µs was A/B-checked against fixed 32-job batches; smaller gaps were not.
const handoffGap = 32_000

// batchBudget is how many jobs a batch needs so that alive workers, each
// finishing a job every jobNanos, come back for hand-offs no more often than
// once per handoffGap; 1 until a batch has been measured. Ten workers on 3 s
// jobs get 1 (so jobs start in queue order), three thousand on 10 ms get 10,
// ten thousand or more hit the cap of 32. The worker about to be handed the
// batch counts even before it is spawned.
func batchBudget(jobNanos int64, alive int) int {
	if jobNanos == 0 {
		return 1
	}

	need := (int64(max(alive, 1))*handoffGap + jobNanos - 1) / jobNanos

	return int(max(1, min(need, maxBatch)))
}

// processNextJob hands the next batch of jobs to one worker. Concurrency
// bounds busy workers, not jobs: a batch occupies one worker however many
// jobs it holds. Jobs are counted before dequeuing so Wait never sees an
// empty queue and zero processing while one is in flight.
func (w *worker[T, JobType]) processNextJob() (bool, error) {
	m := w.manager()
	node, k := w.planBatch(m)
	if node == nil {
		return false, nil // no idle worker, and one more is not worth spawning: wait
	}

	w.busyWorkers.Add(1)
	if !w.reserve(uint32(k)) {
		w.pool.PushNode(node)
		w.busyWorkers.Add(^uint32(0))
		w.finishJobs(uint32(k))
		return false, nil
	}

	buf := node.Value.Batch(k)

	// Parked jobs go first: they were taken before anything in the sources.
	if w.nParked.Load() > 0 {
		if n, stamp := w.unpark(buf); n > 0 {
			if n < k {
				w.finishJobs(uint32(k - n))
			}
			node.Value.SendBatch(buf[:n], stamp)
			return true, nil
		}
	}

	n, cont, err := w.fillBatch(m, buf)

	if n == 0 {
		w.pool.PushNode(node)
		w.busyWorkers.Add(^uint32(0))
		w.finishJobs(uint32(k))
		return cont, err
	}

	if n < k {
		w.finishJobs(uint32(k - n))
	}
	// Stamped after the fill, so a job Added after a Purge returned is never
	// taken for a purged one. Seq orders parkable jobs so they unpark in order.
	// ponytail: a Purge overlapping this fill lets the jobs dequeued before it
	// run (one batch, microseconds). Exact semantics would load the counter
	// before each dequeue and close the stale part of the buffer: one more
	// atomic load per job on the dispatcher, the serial ceiling.
	stamp := pool.Stamp{Gen: m.purgeGen()}
	if stamp.Gen != nil { // a source that batches
		stamp.Epoch = stamp.Gen.Load()
		stamp.Seq = w.dispatched
		w.dispatched += uint64(n)
	}
	node.Value.SendBatch(buf[:n], stamp)

	return cont, err
}

// planBatch picks the worker for the next hand-off and its batch size. The
// backlog is split fairly over every worker the limit allows, so batching
// never costs parallelism; once another goroutine is not worth spawning
// (worthSpawning), it is split over the live ones and none is spawned. The
// share is capped by batchBudget unless concurrency is 1: a lone worker runs
// jobs in queue order anyway and a stop parks what it has not started, so a
// small batch would only cost hand-offs. A nil node means no worker is idle
// and none should be spawned: the loop waits for one to come back.
func (w *worker[T, JobType]) planBatch(m sourceManager[JobType]) (*pool.Elem[JobType], int) {
	pending := w.pending(m)
	alive := int(w.busyWorkers.Load()) + w.pool.Len()

	shares := min(int(w.concurrency.Load()), pending)
	grow := worthSpawning(pending, w.jobNanos.Load(), alive)
	if !grow {
		shares = alive
	}

	node := w.pool.PopBack()
	if node == nil {
		if !grow {
			return nil, 0
		}
		node = w.initPoolNode()
	}

	budget := maxBatch
	if w.concurrency.Load() > 1 {
		budget = batchBudget(w.jobNanos.Load(), alive)
	}

	return node, min(batchSize(pending, shares), budget, m.maxBatch())
}

// fillBatch dequeues into buf, skipping closed jobs (the worker rechecks, as
// a job can be closed while it waits in the batch). It returns the count
// stored, whether to keep dispatching, and any storage error.
func (w *worker[T, JobType]) fillBatch(m sourceManager[JobType], buf []JobType) (n int, cont bool, err error) {
	cont = true
	for n < len(buf) {
		j, nextErr := m.Next()
		if nextErr != nil {
			if errors.Is(nextErr, errNoReadySource) {
				cont = false
			} else {
				err = nextErr
			}
			return
		}
		if j.IsClosed() {
			continue
		}
		buf[n] = j
		n++
	}

	return
}

// reserve counts n jobs as processing, moves an Idle worker to Running, and
// reports whether it is still active; if not, the caller releases the n with
// finishJobs and starts nothing. The count must come first: a Pause that sees
// Running with nothing processing goes straight to Paused, so it either sees
// the count and only reaches Pausing, or halts the worker before the check.
func (w *worker[T, JobType]) reserve(n uint32) bool {
	w.curProcessing.Add(n)
	// Load first: a failed CAS still dirties the line workers read per job.
	if w.status.Load() == idle {
		w.status.CompareAndSwap(idle, running)
	}

	return w.IsActive()
}

// finishJobs takes m jobs off the processing count without w.mx unless they
// were the last ones: waiters are only woken when processing reaches 0. See
// waitForChange for why the decrement may happen before the lock.
func (w *worker[T, JobType]) finishJobs(m uint32) {
	if w.curProcessing.Add(^(m - 1)) == 0 {
		w.mx.Lock()
		// A job may have started since the decrement; then it releases them.
		w.releaseWaiters(w.curProcessing.Load())
		w.mx.Unlock()
	}
}

func (w *worker[T, JobType]) notifyToPullNextJobs() {
	select {
	case w.eventLoopSignal <- struct{}{}:
	default:
	}
}

func (w *worker[T, JobType]) scheduleRetentionCleanup() {
	if w.Configs.stoppedRetention <= 0 {
		return
	}

	if w.Name() == "" {
		return
	}

	w.registryTimer = time.AfterFunc(w.Configs.stoppedRetention, func() {
		w.mx.Lock()
		if w.IsStopped() {
			WorkerRegistry.Delete(w.Name())
		}
		w.mx.Unlock()
	})
}
