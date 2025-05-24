package varmq

import (
	"time"
)

type externalQueue[T, R any] struct {
	*worker[T, R]
}

type IExternalBaseQueue interface {
	// NumPending returns the number of Jobs pending in the queue.
	NumPending() int
	// Purge removes all pending Jobs from the queue.
	// Time complexity: O(n) where n is the number of pending Jobs
	Purge()
	// Close closes the queue and resets all internal states.
	// Time complexity: O(n) where n is the number of channels
	Close() error
}

// IExternalQueue is the root interface of concurrent queue operations.
type IExternalQueue[T, R any] interface {
	IExternalBaseQueue

	// Worker returns the worker.
	Worker() Worker[T, R]
	// WaitUntilFinished waits until all pending Jobs in the queue are processed.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitUntilFinished()
	// WaitAndClose waits until all pending Jobs in the queue are processed and then closes the queue.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitAndClose() error
}

func newExternalQueue[T, R any](worker *worker[T, R]) *externalQueue[T, R] {
	return &externalQueue[T, R]{
		worker: worker,
	}
}

func (eq *externalQueue[T, R]) postEnqueue(j iJob[T, R]) {
	j.ChangeStatus(queued)
	eq.notifyToPullNextJobs()
}

func (eq *externalQueue[T, R]) newJob(data T, configs jobConfigs) *job[T, R] {
	j := newJob[T, R](data, configs)

	if !eq.isNormalWorker() {
		j.withResult(1)
	}

	return j
}

func (eq *externalQueue[T, R]) newGroupJob(len int) *groupJob[T, R] {
	j := newGroupJob[T, R](len)

	if !eq.isNormalWorker() {
		j.withResult(len)
	}

	return j
}

func (eq *externalQueue[T, R]) NumPending() int {
	return eq.Queue.Len()
}

func (eq *externalQueue[T, R]) Worker() Worker[T, R] {
	return eq.worker
}

func (eq *externalQueue[T, R]) WaitUntilFinished() {
	// to ignore deadlock error if the queue is paused
	if eq.IsPaused() {
		eq.Resume()
	}

	eq.wg.Wait()

	// wait until all ongoing processes are done if still pending
	for eq.NumPending() > 0 || eq.NumProcessing() > 0 {
		time.Sleep(10 * time.Millisecond)
	}
}

func (eq *externalQueue[T, R]) Purge() {
	prevValues := eq.Queue.Values()
	eq.Queue.Purge()

	// close all pending channels to avoid routine leaks
	for _, val := range prevValues {
		if j, ok := val.(iJob[T, R]); ok {
			j.CloseResultChannel()
		}
	}
}

func (q *externalQueue[T, R]) Close() error {
	q.Purge()
	q.Stop()
	q.WaitUntilFinished()

	return nil
}

func (q *externalQueue[T, R]) WaitAndClose() error {
	q.WaitUntilFinished()
	return q.Close()
}
