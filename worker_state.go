package varmq

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

func (w *worker[T, JobType]) getStatusError() error {
	switch s := w.status.Load(); s {
	case paused:
		return ErrWorkerPaused
	case stopped:
		return ErrWorkerStopped
	case pausing:
		return ErrWorkerPausing
	case stopping:
		return ErrWorkerStopping
	case running, idle:
		return ErrRunningWorker
	default:
		return ErrNotRunningWorker
	}
}

func (w *worker[T, JobType]) Status() string {
	switch w.status.Load() {
	case initiated:
		return "Initiated"
	case idle:
		return "Idle"
	case running:
		return "Running"
	case pausing:
		return "Pausing"
	case paused:
		return "Paused"
	case stopping:
		return "Stopping"
	case stopped:
		return "Stopped"
	default:
		return "Unknown"
	}
}
