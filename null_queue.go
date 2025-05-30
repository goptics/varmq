package varmq

import (
	"sync"
	"sync/atomic"
)

// nullQueue is a no-op implementation of IQueue interface
type nullQueue struct {
	len atomic.Int64
}

// Initialize a default nullQueue instance
var defaultNullQueue IQueue
var once sync.Once

// getNullQueue returns a singleton instance of nullQueue
func getNullQueue() IQueue {
	once.Do(func() {
		defaultNullQueue = &nullQueue{}
	})

	return defaultNullQueue
}

func (nq *nullQueue) Dequeue() (any, bool) {
	nq.len.Add(-1)
	return nil, false
}

func (nq *nullQueue) Enqueue(item any) bool {
	nq.len.Add(1)
	return false
}

func (nq *nullQueue) Len() int {
	return int(nq.len.Load())
}

func (nq *nullQueue) Values() []any {
	return []any{}
}

func (nq *nullQueue) Purge() {
	nq.len.Store(0)
}

func (nq *nullQueue) Close() error {
	nq.Purge()
	return nil
}
