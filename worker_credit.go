package varmq

// maxCredit caps the jobs one credit pass pops.
const maxCredit = 64

// dispatchCredit serves a worker whose only source is a creditSource: one
// storage call pops as many jobs as there are free slots, and each job goes
// to its own goroutine as it is decoded. Nothing is buffered, so a worker
// never holds a job it has no slot for.
//
// The credit is counted as processing before the pop, as processNextJob
// counts a batch, so Wait never sees zero processing while a pop is in
// flight. The order also closes a race with Pause: a worker whose last job
// finishes after the reserve stays Running, one that went Idle first is set
// back to Running unless a Pause got in, and a Pause that got in first is
// seen by the IsActive check, which releases the credit and pops nothing.
func (w *worker[T, JobType]) dispatchCredit(src creditSource[JobType]) {
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

	w.curProcessing.Add(uint32(credit))
	if w.status.Load() == idle {
		w.status.CompareAndSwap(idle, running)
	}
	if !w.IsActive() {
		w.finishJobs(uint32(credit))
		return
	}

	started := 0
	err := src.fetch(credit, func(j JobType) {
		node := w.pool.PopBack()
		if node == nil {
			node = w.initPoolNode()
		}
		w.busyWorkers.Add(1)
		node.Value.Send(j) // a lone job always runs, even if a Pause lands now
		started++
	}, w.sendError)

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
