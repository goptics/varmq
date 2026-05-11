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
				for w.IsActive() && w.curProcessing.Load() < w.concurrency.Load() && w.queues.Len() > 0 {
					if err := w.processNextJob(); err != nil {
						if errors.Is(err, ErrGetNextQueue) {
							break
						}
						w.sendError(err)
					}
				}
			}
		}
	}(w.ctx, w.eventLoopSignal)
}

func (w *worker[T, JobType]) processNextJob() error {
	queue, err := w.queues.next()

	if err != nil {
		return ErrGetNextQueue
	}

	w.curProcessing.Add(1)
	w.status.CompareAndSwap(idle, running)

	var (
		v     any
		ok    bool
		ackId string
	)

	switch q := queue.(type) {
	case IAcknowledgeable:
		v, ok, ackId = q.DequeueWithAckId()
	default:
		v, ok = q.Dequeue()
	}

	if !ok {
		w.releaseProcessingSlot()
		return ErrFailedToDequeue
	}

	var j JobType

	switch value := v.(type) {
	case JobType:
		j = value
	case []byte:
		var err error
		if v, err = parseToJob[T](value); err != nil {
			w.releaseProcessingSlot()
			return err
		}

		if j, ok = v.(JobType); !ok {
			w.releaseProcessingSlot()
			return ErrFailedToCastJob
		}

		j.setInternalQueue(queue)
	default:
		w.releaseProcessingSlot()
		return ErrFailedToCastJob
	}

	if j.IsClosed() {
		w.releaseProcessingSlot()
		return nil
	}

	j.changeStatus(processing)
	j.setAckId(ackId)

	w.sendToNextChannel(j)

	return nil
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
		WorkerRegistry.Delete(w.Name())
	})
}
