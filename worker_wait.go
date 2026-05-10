package varmq

func (w *worker[T, JobType]) releaseWaiters(processing uint32) {
	if processing != 0 {
		return
	}

	defer w.waiters.Broadcast()

	if w.status.CompareAndSwap(running, idle) {
		return
	}

	if w.status.CompareAndSwap(pausing, paused) {
		return
	}
}

func (w *worker[T, JobType]) Wait() {
	w.mx.Lock()
	defer w.mx.Unlock()

	for {
		s := w.status.Load()
		switch s {
		case running, idle:
			if w.queues.Len() == 0 && w.curProcessing.Load() == 0 {
				return
			}
		case paused, stopped, pausing, stopping:
			if w.curProcessing.Load() == 0 {
				return
			}
		default:
			return
		}
		w.waiters.Wait()
	}
}

func (w *worker[T, JobType]) WaitUntilIdle() {
	w.mx.Lock()
	defer w.mx.Unlock()

	for {
		if w.status.Load() == idle && w.queues.Len() == 0 && w.curProcessing.Load() == 0 {
			return
		}

		w.waiters.Wait()
	}
}

func (w *worker[T, JobType]) WaitUntilPaused() {
	w.mx.Lock()
	defer w.mx.Unlock()

	for w.status.Load() != paused {
		w.waiters.Wait()
	}
}

func (w *worker[T, JobType]) WaitUntilStopped() {
	w.mx.Lock()
	defer w.mx.Unlock()

	for w.status.Load() != stopped {
		w.waiters.Wait()
	}
}

func (w *worker[T, JobType]) PauseAndWait() error {
	if err := w.Pause(); err != nil {
		return err
	}

	w.WaitUntilPaused()
	return nil
}

func (w *worker[T, JobType]) WaitAndPause() error {
	w.Wait()

	return w.Pause()
}

func (w *worker[T, JobType]) StopAndWait() error {
	if err := w.Stop(); err != nil {
		return err
	}

	w.WaitUntilStopped()
	return nil
}

func (w *worker[T, JobType]) WaitAndStop() error {
	w.Wait()
	return w.Stop()
}
