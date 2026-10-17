package varmq

import (
	"context"
	"errors"
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
		start, ran := time.Now(), 0
		for i, j := range jobs {
			// Purged after it was taken: close it as Purge would have. Close
			// fails only if the user closed it meanwhile; Purge ignores that too.
			if stamp.Stale() {
				_ = j.Close()
				continue
			}
			// Stopping or pausing: the jobs not started yet go back to the
			// front of their queue, still Queued, before the processing count
			// drops so a Wait never finds them missing. A Purge that got there
			// first wins and they close instead. A batch of one from storage
			// that cannot take jobs back has no Src and runs: it was in flight
			// once handed out.
			if stamp.Src != nil && w.halting() {
				if !stamp.Src.(sourceManager[JobType]).requeue(jobs[i:], stamp.Stale) {
					for _, j := range jobs[i:] {
						_ = j.Close()
					}
				}
				break
			}
			// A job stays Queued while it waits in the batch, so the user can
			// still Close it; skip it here like the dispatcher would have.
			if !j.startProcessing() {
				continue
			}

			w.workerFunc(j)

			j.changeStatus(finished)
			// The user may close a finished job first; that is not an error.
			if err := j.Close(); err != nil && !errors.Is(err, ErrJobAlreadyClosed) {
				w.sendError(err)
			}
			w.metrics.incCompleted()
			ran++
		}

		// Over the jobs that ran, so a batch mostly given back does not make
		// jobs look cheaper than they are. At least 1: 0 means "not measured
		// yet" to batchBudget.
		if ran > 0 {
			w.jobNanos.Store(max(1, int64(time.Since(start))/int64(ran)))
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
