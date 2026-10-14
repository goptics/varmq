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
				for w.IsActive() && w.curProcessing.Load() < w.concurrency.Load() && w.pendingLen() > 0 {
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
	src, found := w.currentSource()

	// An empty source is "nothing to do", not a dequeue failure. The manager
	// made this check while picking; a single adapter needs it here.
	if !found || src.Len() == 0 {
		return false, nil
	}

	w.curProcessing.Add(1)
	w.status.CompareAndSwap(idle, running)

	j, err := src.Next()
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

func (w *worker[T, JobType]) releaseProcessingSlot() {
	w.mx.Lock()
	processing := w.curProcessing.Add(^uint32(0))
	w.releaseWaiters(processing)
	w.mx.Unlock()
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
