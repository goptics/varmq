package varmq

func (w *worker[T, JobType]) Start() error {
	w.mx.Lock()
	defer w.mx.Unlock()

	if w.IsActive() {
		return ErrRunningWorker
	}

	s := w.status.Load()
	if s != initiated && s != stopped {
		return w.getStatusError()
	}

	if s == stopped {
		w.initContext()
	}

	if w.registryTimer != nil {
		w.registryTimer.Stop()
		w.registryTimer = nil
	}

	if name := w.Name(); name != "" {
		if old, _ := WorkerRegistry.Swap(name, w); old != any(w) {
			if d, ok := old.(interface{ dropMemQueues() }); ok {
				d.dropMemQueues() // replaced under its name: unlisted
			}
		}
		w.memQueues.Range(func(_, m any) bool { registerQueue(m); return true })
	}

	w.goEventLoop()
	w.goRemoveIdleWorkers()

	w.pool.PushNode(w.initPoolNode())

	w.status.Store(idle)
	w.waiters.Broadcast()
	w.notifyToPullNextJobs()
	w.lifecycleChanged()

	return nil
}

func (w *worker[T, JobType]) Stop() error {
	s := w.status.Load()

	if s == stopped || s == stopping {
		return nil
	}

	for _, from := range []status{running, idle, pausing, paused} {
		if w.status.CompareAndSwap(from, stopping) {
			w.mx.Lock()
			w.cancel()
			w.mx.Unlock()
			w.lifecycleChanged()
			return nil
		}
	}

	return w.getStatusError()
}

func (w *worker[T, JobType]) Pause() error {
	w.mx.Lock()
	defer w.mx.Unlock()

	s := w.status.Load()
	if s == paused || s == pausing {
		return nil
	}

	if w.status.CompareAndSwap(idle, paused) {
		w.waiters.Broadcast()
		w.lifecycleChanged()
		return nil
	}

	if w.status.CompareAndSwap(running, pausing) {
		// Jobs taken, not busy workers: the worker that finishes the last job
		// is what flips Pausing to Paused, and it counts in curProcessing.
		if w.curProcessing.Load() == 0 && w.status.CompareAndSwap(pausing, paused) {
			w.waiters.Broadcast()
		}
		w.lifecycleChanged() // Pausing or Paused; releaseWaiters reports the later Paused
		return nil
	}

	return w.getStatusError()
}

func (w *worker[T, JobType]) Resume() error {
	if w.status.CompareAndSwap(paused, idle) {
		w.mx.Lock()
		w.waiters.Broadcast()
		w.mx.Unlock()
		w.notifyToPullNextJobs()
		w.lifecycleChanged()
		return nil
	}

	if w.IsActive() {
		return nil
	}

	return w.getStatusError()
}

func (w *worker[T, JobType]) Restart() error {
	if err := w.Stop(); err != nil {
		return err
	}

	w.mx.Lock()
	for !w.IsStopped() && !w.IsActive() {
		w.waiters.Wait()
	}

	if w.IsActive() {
		w.mx.Unlock()
		return ErrRunningWorker
	}
	w.mx.Unlock()

	return w.Start()
}

// addMemQueue records one of the worker's in-memory QueueInspectors and
// lists it under GET /queues if the worker is listed under GET /workers;
// Start lists the rest.
func (w *worker[T, JobType]) addMemQueue(m *memQueue) {
	w.memQueues.Store(m.id, m)
	listed := func() bool { v, ok := WorkerRegistry.Load(w.Name()); return ok && v == any(w) }
	if listed() {
		registerQueue(m)
		// a drop that ran between the check and the store missed m
		if !listed() {
			queueRegistry.CompareAndDelete(m.id, m)
		}
	}
}

// dropMemQueues unlists the worker's in-memory queues from GET /queues.
func (w *worker[T, JobType]) dropMemQueues() {
	w.memQueues.Range(func(id, m any) bool { queueRegistry.CompareAndDelete(id, m); return true })
}
