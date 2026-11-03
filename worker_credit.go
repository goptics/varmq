package varmq

// maxCredit caps the jobs one credit pass pops.
const maxCredit = 64

// dispatchCredit serves a worker whose only source is a creditSource: one
// storage call pops as many jobs as there are free slots, and each job goes
// to its own goroutine as it is decoded. Nothing is buffered or parked, so a
// worker never holds a job it does not run: every job a pop took starts, even
// if a TunePool lowers the limit while the pop is in flight (the new limit
// applies from the next pop) or a Pause or Stop lands meanwhile. The jobs are
// unacknowledged, and holding them would let their visibility timeout hand
// them to another consumer.
//
// The credit is counted as processing before the pop, as processNextJob
// counts a batch, so Wait never sees zero processing while a pop is in
// flight. The order also closes a race with Pause: a worker whose last job
// finishes after the reserve stays Running, one that went Idle first is set
// back to Running unless a Pause got in, and a worker that is not Running by
// then makes reserve fail, which releases the credit and pops nothing.
func (w *worker[T, JobType]) dispatchCredit(cm *creditManager[JobType]) {
	src := cm.cs
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
	if !w.reserve(uint32(credit)) {
		cm.unspent.Store(0)
		w.finishJobs(uint32(credit))
		return
	}

	started := 0
	err := src.fetch(credit, func(j JobType) {
		cm.unspent.Add(-1)
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
