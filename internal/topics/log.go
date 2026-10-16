// Package topics implements an in-memory append-only log read by independent
// cursors. It is the storage behind varmq's in-memory Topic.
package topics

import (
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
)

const (
	// chunkCapacity is the fixed size of every chunk. The log frees memory a
	// whole chunk at a time, once the slowest cursor has passed it, so chunks
	// stay small instead of growing like the queue's: a grown chunk would keep
	// up to that many read payloads alive. One allocation per 1024 appends.
	chunkCapacity = 1024
)

// chunk is one block of the log, holding the items at offsets base to
// base+len(data). A writer that claimed offset o fills data[o-base] and then
// publishes it by setting its ready flag; a reader reads a slot only once it
// is ready, so readers and writers never touch the same slot at once. Writers
// claim in order but may finish out of order. next is linked by the writer
// that claims the first offset past the end.
type chunk[T any] struct {
	base  uint64
	data  []T
	ready []atomic.Bool
	next  atomic.Pointer[chunk[T]]
}

func newChunk[T any](base uint64) *chunk[T] {
	return &chunk[T]{base: base, data: make([]T, chunkCapacity), ready: make([]atomic.Bool, chunkCapacity)}
}

// end returns the offset just past the chunk.
func (ch *chunk[T]) end() uint64 {
	return ch.base + uint64(len(ch.data))
}

// Log is an append-only, in-memory log stored in linked chunks. Each Cursor
// reads at its own position. The log keeps only the write chunk; chunks link
// forward, so a chunk stays reachable only while a cursor sits on it or on an
// earlier one. Once the slowest cursor moves past a chunk nothing references
// it and the garbage collector frees it. Appending never copies: a full chunk
// links a new one.
//
// Reads never block writes and writes never block reads. An Append to an
// unbounded log takes no lock: it claims an offset with one atomic add on
// head and writes its slot. So the chain of chunks is never cut or replaced
// once started, or a claimed slot could land in a chunk no cursor reaches.
// Appends to a bounded log serialise on mx, so the bound holds exactly.
// Dequeue takes only its own cursor's lock.
type Log[T any] struct {
	mx     sync.Mutex               // cursor bookkeeping (open, limit) and bounded Appends
	tail   atomic.Pointer[chunk[T]] // newest chunk, or one behind while it is linked; nil until the first cursor
	open   []*Cursor[T]             // open cursors
	nOpen  atomic.Int32             // len(open), for Append
	limit  atomic.Uint64            // max unread items for the slowest cursor; 0 means unbounded
	head   atomic.Uint64            // offsets ever claimed, so the offset of the next item
	closed atomic.Bool              // Close was called: Append stores nothing
}

// Cursor reads a Log from its own position. One goroutine reads it at a time;
// mx only orders that reader against Values, Purge and Close.
type Cursor[T any] struct {
	log     *Log[T]
	mx      sync.Mutex
	chunk   *chunk[T] // holds the next unread item; nil once closed
	idx     int       // index into chunk.data; may run past it until the next chunk is reached
	offset  atomic.Uint64
	closed  atomic.Bool
	consume bool // the log's only cursor: clear each slot as it is read
}

// NewLog returns an empty, unbounded log.
func NewLog[T any]() *Log[T] {
	return &Log[T]{}
}

// NewBoundedLog returns an empty log that refuses an Append while its slowest
// open cursor already has limit unread items. A limit below 1 is unbounded.
func NewBoundedLog[T any](limit int) *Log[T] {
	l := &Log[T]{}
	l.limit.Store(uint64(max(0, limit)))

	return l
}

// SetLimit changes the bound NewBoundedLog sets. A limit below 1 is
// unbounded.
func (l *Log[T]) SetLimit(limit int) {
	l.mx.Lock()
	defer l.mx.Unlock()

	l.limit.Store(uint64(max(0, limit)))
}

// Close makes every later Append store nothing; cursors keep reading what was
// stored. On a bounded log an Append already waiting for the lock when Close
// runs is rejected too.
func (l *Log[T]) Close() {
	l.closed.Store(true)
}

// Append stores item once and returns how many open cursors will read it. It
// stores nothing and returns 0 when the log is closed, no cursor is open, or
// the log is bounded and its slowest cursor already has limit unread items.
func (l *Log[T]) Append(item T) int {
	if l.limit.Load() != 0 {
		l.mx.Lock()
		defer l.mx.Unlock()

		// closed is read under the lock so an Append that waited for it
		// observes a Close made meanwhile.
		if l.closed.Load() || len(l.open) == 0 || l.full() {
			return 0
		}
	}

	n := l.nOpen.Load()
	if l.closed.Load() || n == 0 {
		return 0
	}

	// Load tail before claiming: tail only reaches a chunk once its base is
	// claimed, so t.base <= o.
	t := l.tail.Load()
	o := l.head.Add(1) - 1

	for o >= t.end() {
		next := t.next.Load()
		if next == nil {
			if o != t.end() {
				runtime.Gosched() // the Append that claimed t.end() links it
				continue
			}

			next = newChunk[T](o)
			t.next.Store(next)
		}

		l.tail.CompareAndSwap(t, next)
		t = next
	}

	t.data[o-t.base] = item
	t.ready[o-t.base].Store(true) // publish: readers may read the slot from here on

	return int(n)
}

// Full reports whether an Append would be refused for lack of room: the log
// is bounded and its slowest open cursor already has limit unread items. It is
// false with no open cursor.
func (l *Log[T]) Full() bool {
	l.mx.Lock()
	defer l.mx.Unlock()

	return len(l.open) > 0 && l.full()
}

// full reports whether the slowest open cursor is limit items behind. Callers
// hold mx and have at least one open cursor. Offsets only grow, so a stale
// read can refuse an Append early but never lets one through late.
//
// ponytail: O(open cursors) per Append on a bounded log, fine for tens of
// subscribers; keep the minimum offset in a heap if that ever shows up.
func (l *Log[T]) full() bool {
	limit := l.limit.Load()
	if limit == 0 {
		return false
	}

	slowest := l.open[0].offset.Load()
	for _, c := range l.open[1:] {
		slowest = min(slowest, c.offset.Load())
	}

	return l.head.Load()-slowest >= limit
}

// NewCursor returns a cursor positioned at the head: it reads only items
// appended after this call.
func (l *Log[T]) NewCursor() *Cursor[T] {
	l.mx.Lock()
	defer l.mx.Unlock()

	if l.tail.Load() == nil {
		l.tail.Store(newChunk[T](l.head.Load()))
	}

	c := &Cursor[T]{log: l}
	c.seekHead()
	l.open = append(l.open, c)
	l.nOpen.Add(1) // after tail is set: an Append that sees a cursor sees a tail

	return c
}

// seekHead moves the cursor to the head. Appends racing it land on one side
// or the other: those that claimed before head was read are skipped. Callers
// hold c.mx or own c.
func (c *Cursor[T]) seekHead() {
	l := c.log
	tail := l.tail.Load() // before head, so tail.base <= head
	head := l.head.Load()

	c.chunk, c.idx = tail, int(head-tail.base)
	c.offset.Store(head)
}

// NewConsumer returns a cursor like NewCursor that also clears each slot as it
// reads it, so a read item is freed at once instead of with its chunk. It
// turns the log into a FIFO queue and must be the log's only cursor: other
// cursors would find cleared slots.
func (l *Log[T]) NewConsumer() *Cursor[T] {
	c := l.NewCursor()
	c.consume = true

	return c
}

// Len returns how many items the cursor has not read. It is 0 once closed. It
// takes no lock.
func (c *Cursor[T]) Len() int {
	if c.closed.Load() {
		return 0
	}

	return int(max(0, int64(c.log.head.Load()-c.offset.Load())))
}

// Dequeue returns the next unread item and advances the cursor. Stepping off
// a fully read chunk drops this cursor's reference to it. It never takes the
// log's lock.
func (c *Cursor[T]) Dequeue() (T, bool) {
	c.mx.Lock()
	defer c.mx.Unlock()

	var zero T

	if c.chunk == nil {
		return zero, false
	}

	for {
		if c.idx >= len(c.chunk.data) {
			next := c.chunk.next.Load()
			if next == nil {
				if !c.claimed() {
					return zero, false
				}
				runtime.Gosched() // claimed past the end: its chunk is being linked
				continue
			}

			c.chunk, c.idx = next, c.idx-len(c.chunk.data)
			continue
		}

		if !c.chunk.ready[c.idx].Load() {
			if !c.claimed() {
				return zero, false
			}
			// Claimed: its writer is between the claim and the publish, a few
			// instructions, unless it was descheduled.
			runtime.Gosched()
			continue
		}

		item := c.chunk.data[c.idx]
		if c.consume {
			c.chunk.data[c.idx] = zero // a published slot is never written again, so no race
		}
		c.idx++
		c.offset.Add(1)

		return item, true
	}
}

// claimed reports whether an Append has claimed the cursor's next offset, so
// the item there is coming.
func (c *Cursor[T]) claimed() bool {
	return c.offset.Load() < c.log.head.Load()
}

// Values returns a copy of the unread items without consuming them. It stops
// at the first item still being written.
func (c *Cursor[T]) Values() []T {
	c.mx.Lock()
	defer c.mx.Unlock()

	if c.chunk == nil {
		return nil
	}

	out := make([]T, 0, c.Len())
	for ch, i := c.chunk, c.idx; ch != nil; ch, i = ch.next.Load(), i-len(ch.data) {
		for ; i < len(ch.data); i++ {
			if !ch.ready[i].Load() {
				return out
			}
			out = append(out, ch.data[i])
		}
	}

	return out
}

// Purge skips every unread item.
//
// ponytail: a consumer's skipped items in the chunk it lands on stay alive
// until it reads past that chunk (at most chunkCapacity-1); clearing them
// would race Appends still filling claimed slots there.
func (c *Cursor[T]) Purge() {
	c.mx.Lock()
	defer c.mx.Unlock()

	if c.chunk == nil {
		return
	}

	c.seekHead()
}

// Close detaches the cursor so it no longer holds chunks. It is idempotent.
// The log keeps its write chunk even when no cursor is left: an Append racing
// the close may still be filling it.
func (c *Cursor[T]) Close() {
	c.mx.Lock()
	defer c.mx.Unlock()

	if c.chunk == nil {
		return
	}

	c.closed.Store(true)
	c.chunk = nil

	l := c.log
	l.mx.Lock()
	defer l.mx.Unlock()

	l.open = slices.DeleteFunc(l.open, func(o *Cursor[T]) bool { return o == c })
	l.nOpen.Store(int32(len(l.open)))
}
