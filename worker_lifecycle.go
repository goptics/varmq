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
