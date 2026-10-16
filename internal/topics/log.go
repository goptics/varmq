// Package topics implements an in-memory append-only log read by independent
// cursors. It is the storage behind varmq's in-memory Topic.
package topics

import (
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

// chunk is one block of the log. The writer fills data[n] and then publishes
// it by storing n+1; a reader loads n and reads only below it, so readers and
// the writer never touch the same slot. next is linked only once the chunk is
// full.
type chunk[T any] struct {
	data []T
	n    atomic.Int64 // published items
	next atomic.Pointer[chunk[T]]
}

func newChunk[T any](capacity int) *chunk[T] {
	return &chunk[T]{data: make([]T, capacity)}
}

// Log is an append-only, in-memory log stored in linked chunks. Each Cursor
// reads at its own position. The log keeps only the write chunk; chunks link
// forward, so a chunk stays reachable only while a cursor sits on it or on an
// earlier one. Once the slowest cursor moves past a chunk nothing references
// it and the garbage collector frees it. Appending never copies: a full chunk
// links a new one.
//
// Reads never block writes and writes never block reads. Append serialises
// only with other Appends and with opening, closing or purging a cursor;
// Dequeue takes only its own cursor's lock.
type Log[T any] struct {
	mx     sync.Mutex    // writers and cursor bookkeeping: tail, open, limit
	tail   *chunk[T]     // write chunk; nil while no cursor is open
	open   []*Cursor[T]  // open cursors
	limit  uint64        // max unread items for the slowest cursor; 0 means unbounded
	head   atomic.Uint64 // items ever published, so the offset of the next one
	closed atomic.Bool   // Close was called: Append stores nothing
}

// Cursor reads a Log from its own position. One goroutine reads it at a time;
// mx only orders that reader against Values, Purge and Close.
type Cursor[T any] struct {
	log     *Log[T]
	mx      sync.Mutex
	chunk   *chunk[T] // holds the next unread item; nil once closed
	idx     int       // index into chunk.data
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
	return &Log[T]{limit: uint64(max(0, limit))}
}

// SetLimit changes the bound NewBoundedLog sets. A limit below 1 is
// unbounded.
func (l *Log[T]) SetLimit(limit int) {
	l.mx.Lock()
	defer l.mx.Unlock()

	l.limit = uint64(max(0, limit))
}

// Close makes every later Append store nothing. An Append already waiting for
// the log's lock when Close runs is rejected too; cursors keep reading what
// was stored.
func (l *Log[T]) Close() {
	l.closed.Store(true)
}

// Append stores item once and returns how many open cursors will read it. It
// stores nothing and returns 0 when the log is closed, no cursor is open, or
// the log is bounded and its slowest cursor already has limit unread items.
func (l *Log[T]) Append(item T) int {
	l.mx.Lock()
	defer l.mx.Unlock()

	// closed is read under the lock so an Append that waited for it observes
	// a Close made meanwhile.
	if l.closed.Load() || len(l.open) == 0 || l.full() {
		return 0
	}

	n := l.tail.n.Load()
	if int(n) == len(l.tail.data) {
		next := newChunk[T](chunkCapacity)
		l.tail.next.Store(next)
		l.tail, n = next, 0
	}

	l.tail.data[n] = item
	l.tail.n.Store(n + 1) // publish: readers may read data[n] from here on
	l.head.Add(1)         // after publishing, so Len never promises an item Dequeue cannot see

	return len(l.open)
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
	if l.limit == 0 {
		return false
	}

	slowest := l.open[0].offset.Load()
	for _, c := range l.open[1:] {
		slowest = min(slowest, c.offset.Load())
	}

	return l.head.Load()-slowest >= l.limit
}

// NewCursor returns a cursor positioned at the head: it reads only items
// appended after this call.
func (l *Log[T]) NewCursor() *Cursor[T] {
	l.mx.Lock()
	defer l.mx.Unlock()

	if l.tail == nil {
		l.tail = newChunk[T](chunkCapacity)
	}

	c := &Cursor[T]{log: l, chunk: l.tail, idx: int(l.tail.n.Load())}
	c.offset.Store(l.head.Load())
	l.open = append(l.open, c)

	return c
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
		if c.idx < int(c.chunk.n.Load()) {
			item := c.chunk.data[c.idx]
			if c.consume {
				c.chunk.data[c.idx] = zero // the writer never writes below n, so no race
			}
			c.idx++
			c.offset.Add(1)

			return item, true
		}

		// Move on only from a fully read chunk: next is linked once a chunk
		// is full, and a partly read chunk may still be filling.
		next := c.chunk.next.Load()
		if c.idx < len(c.chunk.data) || next == nil {
			return zero, false
		}

		c.chunk, c.idx = next, 0
	}
}

// Values returns a copy of the unread items without consuming them.
func (c *Cursor[T]) Values() []T {
	c.mx.Lock()
	defer c.mx.Unlock()

	if c.chunk == nil {
		return nil
	}

	out := make([]T, 0, c.Len())
	for ch, i := c.chunk, c.idx; ch != nil; ch, i = ch.next.Load(), 0 {
		out = append(out, ch.data[i:ch.n.Load()]...)
	}

	return out
}

// Purge skips every unread item.
func (c *Cursor[T]) Purge() {
	c.mx.Lock()
	defer c.mx.Unlock()

	if c.chunk == nil {
		return
	}

	l := c.log
	l.mx.Lock() // tail, its count and head move together only under the writer lock
	defer l.mx.Unlock()

	if c.consume {
		// The only reader: drop the stored items with their chunks.
		l.tail = newChunk[T](chunkCapacity)
	}

	c.chunk, c.idx = l.tail, int(l.tail.n.Load())
	c.offset.Store(l.head.Load())
}

// Close detaches the cursor so it no longer holds chunks. When it was the
// last open cursor the log drops its write chunk too. It is idempotent.
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
	if len(l.open) == 0 {
		l.tail = nil
	}
}
