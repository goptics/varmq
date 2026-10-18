package varmq

import "time"

// waitRecheck is how often Wait and WaitUntilIdle re-check while the worker
// runs no job. See waitForChange.
const waitRecheck = 10 * time.Millisecond

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
			if w.NumPending() == 0 && w.curProcessing.Load() == 0 {
				return
			}
		case paused, stopped, pausing, stopping:
			if w.curProcessing.Load() == 0 {
				return
			}
		default:
			return
		}
		w.waitForChange()
	}
}

// waitForChange blocks until the waiters are woken. Callers hold w.mx.
//
// The job that brings the running count to 0 broadcasts under w.mx after the
// decrement, so a caller either sees 0 or is already waiting. But a source
// shared with other workers or processes (one queue bound to several workers,
// a persistent or distributed queue) can be drained elsewhere while this
// worker runs nothing, and then nothing wakes its waiters. So while no job
// runs, a timer also wakes them after waitRecheck. It decides on the running
// count alone: NumPending may have changed since the caller checked.
func (w *worker[T, JobType]) waitForChange() {
	if w.curProcessing.Load() == 0 {
		t := time.AfterFunc(waitRecheck, func() {
			w.mx.Lock()
			w.waiters.Broadcast()
			w.mx.Unlock()
		})
		defer t.Stop()
	}

	w.waiters.Wait()
}

func (w *worker[T, JobType]) WaitUntilIdle() {
	w.mx.Lock()
	defer w.mx.Unlock()

	for {
		if w.status.Load() == idle && w.NumPending() == 0 && w.curProcessing.Load() == 0 {
			return
		}

		w.waitForChange()
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
