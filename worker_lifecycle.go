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
		w.initContext(w.Configs.ctx)
	}

	w.goEventLoop()
	w.goRemoveIdleWorkers()

	w.pool.PushNode(w.initPoolNode())

	w.status.Store(idle)
	w.waiters.Broadcast()
	w.notifyToPullNextJobs()

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
		return nil
	}

	if w.status.CompareAndSwap(running, pausing) {
		if w.NumProcessing() == 0 && w.status.CompareAndSwap(pausing, paused) {
			w.waiters.Broadcast()
		}
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
