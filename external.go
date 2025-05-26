package varmq

import (
	"io"
	"time"
)

type externalQueue struct {
	w Worker
}

type IExternalBaseQueue interface {
	PendingTracker
	// Purge removes all pending Jobs from the queue.
	// Time complexity: O(n) where n is the number of pending Jobs
	Purge()
	// Close closes the queue and resets all internal states.
	// Time complexity: O(n) where n is the number of channels
	Close() error
}

// IExternalQueue is the root interface of concurrent queue operations.
type IExternalQueue interface {
	IExternalBaseQueue

	// Worker returns the worker.
	Worker() Worker
	// WaitUntilFinished waits until all pending Jobs in the queue are processed.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitUntilFinished()
	// WaitAndClose waits until all pending Jobs in the queue are processed and then closes the queue.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitAndClose() error
}

func newExternalQueue(worker Worker) *externalQueue {
	return &externalQueue{
		w: worker,
	}
}

func (eq *externalQueue) NumPending() int {
	return eq.w.queue().Len()
}

func (eq *externalQueue) Worker() Worker {
	return eq.w
}

func (eq *externalQueue) WaitUntilFinished() {
	// to ignore deadlock error if the queue is paused
	if eq.w.IsPaused() {
		eq.w.Resume()
	}

	eq.w.wait()

	// wait until all ongoing processes are done if still pending
	for eq.NumPending() > 0 || eq.w.NumProcessing() > 0 {
		time.Sleep(10 * time.Millisecond)
	}
}

func (eq *externalQueue) Purge() {
	prevValues := eq.w.queue().Values()
	eq.w.queue().Purge()

	// close all pending channels to avoid routine leaks
	for _, val := range prevValues {
		if j, ok := val.(io.Closer); ok {
			j.Close()
		}
	}
}

func (q *externalQueue) Close() error {
	q.Purge()
	q.w.Stop()
	q.WaitUntilFinished()

	return nil
}

func (q *externalQueue) WaitAndClose() error {
	q.WaitUntilFinished()
	return q.Close()
}
