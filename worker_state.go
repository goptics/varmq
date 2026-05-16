package varmq

type status = uint32

const (
	initiated status = iota
	idle
	running
	pausing
	paused
	stopping
	stopped
)

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

func (w *worker[T, JobType]) IsPaused() bool {
	return w.status.Load() == paused
}

func (w *worker[T, JobType]) IsRunning() bool {
	return w.status.Load() == running
}

func (w *worker[T, JobType]) IsActive() bool {
	s := w.status.Load()
	return s == running || s == idle
}

func (w *worker[T, JobType]) IsIdle() bool {
	return w.status.Load() == idle
}

func (w *worker[T, JobType]) IsStopped() bool {
	return w.status.Load() == stopped
}
