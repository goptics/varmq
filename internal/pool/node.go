package pool

import (
	"sync/atomic"
	"time"
)

type Payload[T any] struct {
	data T
	ok   bool
}

type Node[T any] struct {
	ch       chan Payload[T]
	lastUsed atomic.Value
}

func NewNode[T any](bufferSize int) *Node[T] {
	node := &Node[T]{
		ch: make(chan Payload[T], bufferSize),
	}

	return node
}

func (wc *Node[T]) Send(data T) {
	wc.ch <- Payload[T]{
		data: data,
		ok:   true,
	}
}

func (wc *Node[T]) Serve(fn func(T)) {
	for payload := range wc.ch {
		if !payload.ok {
			return
		}

		fn(payload.data)
	}
}

func (wc *Node[T]) Stop() {
	wc.ch <- Payload[T]{
		ok: false,
	}
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
