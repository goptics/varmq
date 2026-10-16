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
				for w.IsActive() && w.curProcessing.Load() < w.concurrency.Load() && w.NumPending() > 0 {
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

func (w *worker[T, JobType]) processNextJob() (bool, error) {
	w.curProcessing.Add(1)
	w.status.CompareAndSwap(idle, running)

	j, err := w.manager().Next()
	if err != nil {
		w.releaseProcessingSlot()
		if errors.Is(err, errNoReadySource) {
			return false, nil
		}
		return true, err
	}

	if j.IsClosed() {
		w.releaseProcessingSlot()
		return true, nil
	}

	j.changeStatus(processing)

	w.sendToNextChannel(j)

	return true, nil
}

// releaseProcessingSlot frees a slot without w.mx unless it was the last one:
// waiters are only woken when processing reaches 0. See waitForChange for why
// the decrement may happen before the lock.
func (w *worker[T, JobType]) releaseProcessingSlot() {
	processing := w.curProcessing.Add(^uint32(0))
	if processing == 0 {
		w.mx.Lock()
		// A job may have started since the decrement; then it releases them.
		w.releaseWaiters(w.curProcessing.Load())
		w.mx.Unlock()
	}
	if processing < w.concurrency.Load() {
		w.notifyToPullNextJobs()
	}
}

func (w *worker[T, JobType]) sendToNextChannel(j JobType) {
	if node := w.pool.PopBack(); node != nil {
		node.Value.Send(j)
		return
	}

	w.initPoolNode().Value.Send(j)
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
