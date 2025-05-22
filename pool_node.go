package varmq

import (
	"sync/atomic"
	"time"
)

type poolNode[T, R any] struct {
	ch       chan iJob[T, R]
	lastUsed atomic.Value
}

// newPoolNode creates a new pool node with initialized lastUsed field
func newPoolNode[T, R any](bufferSize int) poolNode[T, R] {
	node := poolNode[T, R]{
		ch: make(chan iJob[T, R], bufferSize),
	}
	return node
}

func (wc *poolNode[T, R]) Close() {
	close(wc.ch)
}

func (wc *poolNode[T, R]) UpdateLastUsed() {
	wc.lastUsed.Store(time.Now())
}

// GetLastUsed safely retrieves the lastUsed time
func (wc *poolNode[T, R]) GetLastUsed() time.Time {
	if val := wc.lastUsed.Load(); val != nil {
		return val.(time.Time)
	}
	// Return zero time if the value hasn't been initialized
	return time.Time{}
}
