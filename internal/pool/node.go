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
	stamp Stamp
	ok    bool
}

// Stamp records the value a generation counter had when a batch was taken.
// The batch goes stale once the counter moves on. A nil Gen never does.
// Seq is the dispatch number of the batch's first job, so batches set aside
// by different workers can be put back in the order they were handed out.
type Stamp struct {
	Gen   *atomic.Uint64
	Epoch uint64
	Seq   uint64
}

// Stale reports whether the counter has moved since the batch was stamped.
func (s Stamp) Stale() bool {
	return s.Gen != nil && s.Gen.Load() != s.Epoch
}

type Node[T any] struct {
	ch       chan Payload[T]
	buf      []T   // batch staging; owned by whoever popped the node until Serve has read it
	single   [1]T  // lets Serve hand a lone Send to fn as a slice without allocating
	lastUsed int64 // unix nanos of the last UpdateLastUsed, read and written atomically; 0 until then
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

// SendBatch hands jobs, a slice from Batch, to the worker in one wake-up,
// along with the stamp Serve passes to fn.
func (wc *Node[T]) SendBatch(jobs []T, stamp Stamp) {
	wc.ch <- Payload[T]{
		batch: jobs,
		stamp: stamp,
		ok:    true,
	}
}

// Serve runs fn once per payload with the items it carries, one from Send or
// a whole batch from SendBatch, and its stamp (zero for Send). The slice is the node's own storage: fn must
// clear it before it lets anyone else (a pool) hand the node a new batch.
func (wc *Node[T]) Serve(fn func([]T, Stamp)) {
	for payload := range wc.ch {
		if !payload.ok {
			return
		}

		items := payload.batch
		if items == nil {
			wc.single[0] = payload.data
			items = wc.single[:]
		}

		fn(items, payload.stamp)
	}
}

func (wc *Node[T]) Stop() {
	wc.ch <- Payload[T]{
		ok: false,
	}
}

// UpdateLastUsed records now. An integer, not an atomic.Value holding a
// time.Time, so the per-batch call does not allocate. A plain int64 with
// atomic.StoreInt64 rather than atomic.Int64 because CreateNode returns the
// Node by value and atomic.Int64 must not be copied.
func (wc *Node[T]) UpdateLastUsed() {
	atomic.StoreInt64(&wc.lastUsed, time.Now().UnixNano())
}

// GetLastUsed returns the last UpdateLastUsed time, or the zero time.
func (wc *Node[T]) GetLastUsed() time.Time {
	ns := atomic.LoadInt64(&wc.lastUsed)
	if ns == 0 {
		return time.Time{}
	}

	return time.Unix(0, ns)
}
