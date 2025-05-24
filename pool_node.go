package varmq

import (
	"sync/atomic"
	"time"
)

type poolNode[T any] struct {
	ch       chan T
	lastUsed atomic.Value
}

// newPoolNode creates a new pool node with initialized lastUsed field
func newPoolNode[T any](bufferSize int) poolNode[T] {
	node := poolNode[T]{
		ch: make(chan T, bufferSize),
	}
	return node
}

func (wc *poolNode[T]) Close() {
	close(wc.ch)
}

func (wc *poolNode[T]) UpdateLastUsed() {
	wc.lastUsed.Store(time.Now())
}

// GetLastUsed safely retrieves the lastUsed time
func (wc *poolNode[T]) GetLastUsed() time.Time {
	if val := wc.lastUsed.Load(); val != nil {
		return val.(time.Time)
	}
	// Return zero time if the value hasn't been initialized
	return time.Time{}
}
