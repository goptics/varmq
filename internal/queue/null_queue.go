package queue

import "sync/atomic"

// nullQueue is a no-op implementation of IQueue interface
type nullQueue struct {
	len atomic.Int64
}

// Initialize a default nullQueue instance
var defaultNullQueue IQueue

// getNullQueue returns a singleton instance of nullQueue
func GetNullQueue() IQueue {
	if defaultNullQueue != nil {
		return defaultNullQueue
	}

	defaultNullQueue = &nullQueue{}
	return defaultNullQueue
}

func (nq *nullQueue) NotificationChannel() <-chan struct{} {
	return nil
}

func (nq *nullQueue) Dequeue() (any, bool) {
	nq.len.Add(-1)
	return nil, false
}

func (nq *nullQueue) Enqueue(item any) bool {
	nq.len.Add(1)
	return false
}

func (nq *nullQueue) Init() {}

func (nq *nullQueue) Len() int {
	return int(nq.len.Load())
}

func (nq *nullQueue) Values() []any {
	return []any{}
}

func (nq *nullQueue) Close() error {
	return nil
}
