package varmq

import "sync/atomic"

type Metrics interface {
	// Submitted returns the total number of tasks submitted to the worker.
	Submitted() uint64
	// Completed returns the total number of tasks that have completed processing
	// (both successful and failed).
	Completed() uint64
	// Successful returns the total number of tasks that completed successfully.
	Successful() uint64
	// Failed returns the total number of tasks that failed (returned an error or panicked).
	Failed() uint64
	// Reset resets all counters to zero.
	Reset()

	incSubmitted()
	incCompleted()
	incSuccessful()
	incFailed()
}

type metrics struct {
	submitted  atomic.Uint64
	completed  atomic.Uint64
	successful atomic.Uint64
	failed     atomic.Uint64
}

// newMetrics creates a new Metrics instance with all counters initialized to zero.
func newMetrics() Metrics {
	return &metrics{}
}

func (m *metrics) Submitted() uint64 {
	return m.submitted.Load()
}

func (m *metrics) Completed() uint64 {
	return m.completed.Load()
}

func (m *metrics) Successful() uint64 {
	return m.successful.Load()
}

func (m *metrics) Failed() uint64 {
	return m.failed.Load()
}

func (m *metrics) incSubmitted() {
	m.submitted.Add(1)
}

func (m *metrics) incCompleted() {
	m.completed.Add(1)
}

func (m *metrics) incSuccessful() {
	m.successful.Add(1)
}

func (m *metrics) incFailed() {
	m.failed.Add(1)
}

func (m *metrics) Reset() {
	m.submitted.Store(0)
	m.completed.Store(0)
	m.successful.Store(0)
	m.failed.Store(0)
}
