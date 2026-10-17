package varmq

import (
	"context"
	"errors"
	"time"
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

// fastJobNanos is the per-job cost below which jobs count as cheaper than a
// hand-off, so one worker may take several while others could be spawned.
const fastJobNanos = 10_000

// batchSize is one worker's share of the backlog among the live workers,
// busy or idle, at least 1 and at most maxBatch. Dividing by the live count
// rather than the free count matters at both ends: with few goroutines
// spawned yet (a high limit, cheap jobs) the share is large and the wake-up
// cost is spread over the batch; with every worker busy the share stays
// small, so no worker ends up holding a long run of jobs while the others
// find the queue empty and stop.
func batchSize(pending, alive int) int {
	return max(1, min(pending/max(alive, 1), maxBatch))
}

// processNextJob hands the next batch of jobs to one worker. Concurrency
// bounds busy workers, not jobs: a batch occupies one worker however many
// jobs it holds. Jobs are counted before dequeuing so Wait never sees an
// empty queue and zero processing while one is in flight.
func (w *worker[T, JobType]) processNextJob() (bool, error) {
	w.status.CompareAndSwap(idle, running)

	m := w.manager()
	pending := m.Len()
	// Fair share: every worker the limit allows gets its own job before
	// anyone gets two, so a batch never costs parallelism. Once jobs have
	// proven cheaper than a hand-off, share among the live goroutines
	// instead: spawning more just to hand each one job would be slower.
	divisor := min(int(w.concurrency.Load()), pending)
	if ns := w.jobNanos.Load(); ns != 0 && ns < fastJobNanos {
		divisor = int(w.busyWorkers.Load()) + w.pool.Len()
	}
	k := min(batchSize(pending, divisor), m.maxBatch())
	w.busyWorkers.Add(1)
	w.curProcessing.Add(uint32(k))

	node := w.pool.PopBack()
	if node == nil {
		node = w.initPoolNode()
	}
	buf := node.Value.Batch(k)

	n, cont, err := 0, true, error(nil)
	for n < k {
		j, nextErr := m.Next()
		if nextErr != nil {
			if errors.Is(nextErr, errNoReadySource) {
				cont = false
			} else {
				err = nextErr
			}
			break
		}
		if j.IsClosed() {
			continue
		}
		buf[n] = j
		n++
	}

	if n == 0 {
		w.pool.PushNode(node)
		w.busyWorkers.Add(^uint32(0))
		w.finishJobs(uint32(k))
		return cont, err
	}

	if n < k {
		w.finishJobs(uint32(k - n))
	}
	node.Value.SendBatch(buf[:n])

	return cont, err
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
