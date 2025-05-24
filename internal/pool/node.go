package pool

import (
	"sync/atomic"
	"time"
)

type Node[T any] struct {
	ch       chan T
	lastUsed atomic.Value
}

// NewNode creates a new pool node with initialized lastUsed field
func NewNode[T any](bufferSize int) Node[T] {
	node := Node[T]{
		ch: make(chan T, bufferSize),
	}

	return node
}

func (wc *Node[T]) Read() <-chan T {
	return wc.ch
}

func (wc *Node[T]) Send(j T) {
	wc.ch <- j
}

func (wc *Node[T]) Close() {
	close(wc.ch)
}

func (wc *Node[T]) UpdateLastUsed() {
	wc.lastUsed.Store(time.Now())
}

// GetLastUsed safely retrieves the lastUsed time
func (wc *Node[T]) GetLastUsed() time.Time {
	if val := wc.lastUsed.Load(); val != nil {
		return val.(time.Time)
	}
	// Return zero time if the value hasn't been initialized
	return time.Time{}
}
