package pool

import (
	"sync/atomic"
	"time"
)

// Payload is one hand-off to a worker: a single item, or a batch staged in
// the node's Buf. ok false tells the worker to stop.
type Payload[T any] struct {
	data  T
	batch []T
	ok    bool
}

type Node[T any] struct {
	ch       chan Payload[T]
	buf      []T  // batch staging; owned by whoever popped the node until Serve has read it
	single   [1]T // lets Serve hand a lone Send to fn as a slice without allocating
	lastUsed atomic.Value
}

func CreateNode[T any](bufferSize int) Node[T] {
	node := Node[T]{
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

// Batch returns a staging slice of n items for the caller to fill and then
// pass to SendBatch. The slice is reused across batches, so the caller must
// hold the node exclusively (popped from the pool) until the worker has
// consumed it.
func (wc *Node[T]) Batch(n int) []T {
	if cap(wc.buf) < n {
		wc.buf = make([]T, max(n, 2*cap(wc.buf)))
	}

	return wc.buf[:n]
}

// SendBatch hands jobs, a slice from Batch, to the worker in one wake-up.
func (wc *Node[T]) SendBatch(jobs []T) {
	wc.ch <- Payload[T]{
		batch: jobs,
		ok:    true,
	}
}

// Serve runs fn once per payload with the items it carries, one from Send or
// a whole batch from SendBatch. The slice is the node's own storage: fn must
// clear it before it lets anyone else (a pool) hand the node a new batch.
func (wc *Node[T]) Serve(fn func([]T)) {
	for payload := range wc.ch {
		if !payload.ok {
			return
		}

		items := payload.batch
		if items == nil {
			wc.single[0] = payload.data
			items = wc.single[:]
		}

		fn(items)
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
