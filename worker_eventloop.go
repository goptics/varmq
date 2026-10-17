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

// batchSize is each free worker's share of the backlog, at least 1 and at
// most maxBatch. With a shallow backlog it is 1, so no worker sits on jobs
// another could run; with a deep one every worker would be busy anyway and
// the wake-up cost is spread over the batch.
func batchSize(pending, free int) int {
	return max(1, min(pending/max(free, 1), maxBatch))
}

// processNextJob hands the next batch of jobs to one worker. Concurrency
// bounds busy workers, not jobs: a batch occupies one worker however many
// jobs it holds. Jobs are counted before dequeuing so Wait never sees an
// empty queue and zero processing while one is in flight.
func (w *worker[T, JobType]) processNextJob() (bool, error) {
	w.status.CompareAndSwap(idle, running)

	m := w.manager()
	free := int(w.concurrency.Load() - w.busyWorkers.Load())
	k := min(batchSize(m.Len(), free), m.maxBatch())
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
