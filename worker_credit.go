package varmq

import "github.com/goptics/varmq/internal/pool"

// maxCredit caps the jobs one credit pass pops.
const maxCredit = 64

// dispatchCredit serves a worker whose only source is a creditSource: one
// storage call pops as many jobs as there are free slots, and each job goes
// to its own goroutine as it is decoded. Nothing is buffered, so a worker
// never holds a job it has no slot for; if TunePool takes the slot away
// while the pop is in flight, the job is parked for the next free slot.
// Jobs the pop took still run if a Pause or Stop lands meanwhile: they are
// unacknowledged, and holding them through a long pause would let their
// visibility timeout hand them to another consumer.
//
// The credit is counted as processing before the pop, as processNextJob
// counts a batch, so Wait never sees zero processing while a pop is in
// flight. The order also closes a race with Pause: a worker whose last job
// finishes after the reserve stays Running, one that went Idle first is set
// back to Running unless a Pause got in, and a Pause that got in first is
// seen by the IsActive check, which releases the credit and pops nothing.
func (w *worker[T, JobType]) dispatchCredit(cm *creditManager[JobType]) {
	src := cm.src
	if !w.IsActive() {
		return
	}

	if src.ready() == 0 {
		src.armResync() // a Resume ends up here: re-arm a resync stopped by the pause
		return
	}

	// Signed: TunePool may lower concurrency below the busy count.
	free := int(w.concurrency.Load()) - int(w.busyWorkers.Load())
	if free <= 0 {
		return // a finishing job signals again
	}
	credit := min(free, maxCredit)

	cm.unspent.Store(int64(credit)) // first: NumPending subtracts it from the reserve below
	w.curProcessing.Add(uint32(credit))
	if w.status.Load() == idle {
		w.status.CompareAndSwap(idle, running)
	}
	if !w.IsActive() {
		cm.unspent.Store(0)
		w.finishJobs(uint32(credit))
		return
	}

	started, parking := 0, false
	err := src.fetch(credit, func(j JobType) {
		cm.unspent.Add(-1)
		// TunePool lowered concurrency during the pop. Once one job is
		// parked, the rest of the pop follows it, so none starts ahead of it.
		if parking || w.busyWorkers.Load() >= w.concurrency.Load() {
			parking = true
			w.park([]JobType{j}, pool.Stamp{Seq: w.dispatched})
			w.dispatched++
			return
		}
		node := w.pool.PopBack()
		if node == nil {
			node = w.initPoolNode()
		}
		w.busyWorkers.Add(1)
		node.Value.Send(j) // a lone job always runs, even if a Pause lands now
		started++
	}, w.sendError)

	cm.unspent.Store(0)
	if started < credit {
		w.finishJobs(uint32(credit - started))
	}

	if err != nil {
		w.sendError(err)
		return
	}

	if src.ready() > 0 {
		w.notifyToPullNextJobs() // through the select again: a bind since may have swapped the manager
	}
}
