package varmq

import (
	"context"
	"time"

	"github.com/goptics/varmq/internal/linkedlist"
	"github.com/goptics/varmq/internal/pool"
)

func (w *worker[T, JobType]) TunePool(concurrency int) error {
	if !w.IsActive() {
		return w.getStatusError()
	}

	oldConcurrency := w.concurrency.Load()
	safeConcurrency := withSafeConcurrency(concurrency)

	if oldConcurrency == safeConcurrency {
		return ErrSameConcurrency
	}

	w.concurrency.Store(safeConcurrency)

	if safeConcurrency > oldConcurrency {
		w.notifyToPullNextJobs()
		return nil
	}

	if w.Configs.idleWorkerExpiryDuration != 0 {
		return nil
	}

	shrinkPoolSize, minIdleWorkers := oldConcurrency-safeConcurrency, w.numMinIdleWorkers()

	for shrinkPoolSize > 0 && w.pool.Len() != minIdleWorkers {
		if node := w.pool.PopBack(); node != nil {
			w.pool.Remove(node)
			node.Value.Stop()
			w.pool.Cache.Put(node)
			shrinkPoolSize--
		} else {
			break
		}
	}

	return nil
}

func (w *worker[T, JobType]) numMinIdleWorkers() int {
	percentage := w.Configs.minIdleWorkerRatio
	concurrency := w.concurrency.Load()

	return int(max((concurrency*uint32(percentage))/100, 1))
}

func (w *worker[T, JobType]) goRemoveIdleWorkers() {
	interval := w.Configs.idleWorkerExpiryDuration

	if interval == 0 {
		return
	}

	ticker := time.NewTicker(interval)

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				targetIdleWorkers := w.numMinIdleWorkers()

				if w.pool.Len() <= targetIdleWorkers {
					continue
				}

				nodes := w.pool.NodeSlice()
				for _, node := range nodes[targetIdleWorkers:] {
					if node.Value.GetLastUsed().Add(interval).Before(time.Now()) &&
						!(node.Next() == nil && node.Prev() == nil) {
						w.pool.Remove(node)
						node.Value.Stop()
						w.pool.Cache.Put(node)
					}
				}
			}
		}
	}(w.ctx)
}

func (w *worker[T, JobType]) removeAllWorkers() {
	for _, node := range w.pool.NodeSlice() {
		w.pool.Remove(node)
		node.Value.Stop()
		w.pool.Cache.Put(node)
	}
}

func (w *worker[T, JobType]) initPoolNode() *linkedlist.Node[pool.Node[JobType]] {
	node := w.pool.Cache.Get().(*linkedlist.Node[pool.Node[JobType]])

	go node.Value.Serve(func(j JobType) {
		w.workerFunc(j)

		j.changeStatus(finished)
		if err := j.Close(); err != nil {
			w.sendError(err)
		}
		w.metrics.incCompleted()
		w.freePoolNode(node)
		w.releaseProcessingSlot()
	})

	return node
}

func (w *worker[T, JobType]) freePoolNode(node *linkedlist.Node[pool.Node[JobType]]) {
	enabledIdleWorkersRemover := w.Configs.idleWorkerExpiryDuration > 0

	if enabledIdleWorkersRemover {
		node.Value.UpdateLastUsed()
	}

	if w.queues.Len() >= w.NumConcurrency() || enabledIdleWorkersRemover || w.pool.Len() < w.numMinIdleWorkers() {
		w.pool.PushNode(node)
		return
	}

	node.Value.Stop()
	w.pool.Cache.Put(node)
}
