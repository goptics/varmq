package varmq

import (
	"context"
	"time"

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

				expired := w.pool.Prune(targetIdleWorkers, func(node *pool.Elem[JobType]) bool {
					return node.Value.GetLastUsed().Add(interval).Before(time.Now())
				})
				for _, node := range expired {
					node.Value.Stop()
					w.pool.Cache.Put(node)
				}
			}
		}
	}(w.ctx)
}

func (w *worker[T, JobType]) removeAllWorkers() {
	for node := w.pool.PopBack(); node != nil; node = w.pool.PopBack() {
		node.Value.Stop()
		w.pool.Cache.Put(node)
	}
}

func (w *worker[T, JobType]) initPoolNode() *pool.Elem[JobType] {
	node := w.pool.Cache.Get().(*pool.Elem[JobType])

	go node.Value.Serve(func(jobs []JobType, stamp pool.Stamp) {
		start := time.Now()
		for _, j := range jobs {
			// A job stays Queued while it waits in the batch, so the user can
			// still Close it; skip it here like the dispatcher would have.
			if j.IsClosed() {
				continue
			}
			// Purged after it was taken: close it as Purge would have.
			if stamp.Stale() {
				if err := j.Close(); err != nil {
					w.sendError(err)
				}
				continue
			}
			j.changeStatus(processing)

			w.workerFunc(j)

			j.changeStatus(finished)
			if err := j.Close(); err != nil {
				w.sendError(err)
			}
			w.metrics.incCompleted()
		}

		if n := len(jobs); n > 0 {
			w.jobNanos.Store(int64(time.Since(start)) / int64(n))
		}

		// Drop the job references before the node is up for grabs again, then
		// back in the pool and off the busy count before the processing count
		// drops: a Wait that returns must find every idle worker in the pool.
		clear(jobs)
		w.freePoolNode(node)
		w.busyWorkers.Add(^uint32(0))
		w.finishJobs(uint32(len(jobs)))
		w.notifyToPullNextJobs()
	})

	return node
}

func (w *worker[T, JobType]) freePoolNode(node *pool.Elem[JobType]) {
	enabledIdleWorkersRemover := w.Configs.idleWorkerExpiryDuration > 0

	if enabledIdleWorkersRemover {
		node.Value.UpdateLastUsed()
	}

	// Keep the goroutine while there is more backlog than idle workers to
	// run it; stopping it would only make the next dispatch spawn another.
	if w.manager().Len() > w.pool.Len() || enabledIdleWorkersRemover || w.pool.Len() < w.numMinIdleWorkers() {
		w.pool.PushNode(node)
		return
	}

	node.Value.Stop()
	w.pool.Cache.Put(node)
}
