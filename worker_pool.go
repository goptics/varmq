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

	// In uint64: the product passes MaxUint32 once concurrency is in the tens of millions.
	return int(max(uint64(concurrency)*uint64(percentage)/100, 1))
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
			// Stopping or pausing: park the unstarted rest (see park). Parked
			// before the processing count drops, so Paused and Stopped are
			// reached only once they are accounted for. The first job always
			// runs: NumProcessing has counted it since the hand-off, so Stop
			// waits for it. A job from a source that never batches (priority,
			// acknowledging, several sources) has no Gen and runs likewise.
			if i > 0 && stamp.Gen != nil && w.halting() {
				rest := stamp
				rest.Seq += uint64(i)
				w.park(jobs[i:], rest)
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

		// Averaged over the jobs that ran, so a mostly parked batch does not
		// make jobs look cheap. At least 1: 0 means "not measured yet".
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
	if w.pending(w.manager()) > w.pool.Len() || enabledIdleWorkersRemover || w.pool.Len() < w.numMinIdleWorkers() {
		w.pool.PushNode(node)
		return
	}

	node.Value.Stop()
	w.pool.Cache.Put(node)
}
