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
				for w.IsActive() && w.busyWorkers.Load() < w.concurrency.Load() && w.manager().Len() > 0 {
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

// spawnNanos is what one more pool goroutine costs in wall time: the spawn
// itself on an event loop that shares a few cores with everything else,
// plus the stack it allocates and the collector later scans.
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

// processNextJob hands the next batch of jobs to one worker. Concurrency
// bounds busy workers, not jobs: a batch occupies one worker however many
// jobs it holds. Jobs are counted before dequeuing so Wait never sees an
// empty queue and zero processing while one is in flight.
func (w *worker[T, JobType]) processNextJob() (bool, error) {
	w.status.CompareAndSwap(idle, running)

	m := w.manager()
	node, k := w.planBatch(m)
	if node == nil {
		return false, nil // no idle worker, and one more is not worth spawning: wait
	}

	w.busyWorkers.Add(1)
	w.curProcessing.Add(uint32(k))

	buf := node.Value.Batch(k)
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
	// mistaken for a purged one.
	// ponytail: a Purge overlapping this fill lets the jobs dequeued before it
	// run (one batch, microseconds). Exact semantics would load the counter
	// before each dequeue and close the stale part of the buffer: one more
	// atomic load per job on the dispatcher, the serial ceiling.
	stamp := pool.Stamp{Gen: m.purgeGen()}
	if stamp.Gen != nil {
		stamp.Epoch = stamp.Gen.Load()
	}
	node.Value.SendBatch(buf[:n], stamp)

	return cont, err
}

// planBatch picks the worker for the next hand-off and how many jobs it
// gets. The backlog is split by fair share over every worker the limit
// allows, so a batch never costs parallelism; once one more goroutine would
// cost more than it saves (worthSpawning), it is split over the live
// goroutines instead and none is spawned. A nil node means no worker is idle
// and none should be spawned: the loop waits for one to come back.
func (w *worker[T, JobType]) planBatch(m sourceManager[JobType]) (*pool.Elem[JobType], int) {
	pending := m.Len()
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

	return node, min(batchSize(pending, shares), m.maxBatch())
}

// fillBatch dequeues into buf, skipping jobs the user already closed (the
// worker checks again before running one, since a job can be closed while it
// waits in the batch). It returns how many it stored, whether the loop should
// keep dispatching, and any storage error.
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
