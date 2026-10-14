// Package topics implements an in-memory append-only log read by independent
// cursors. It is the storage behind varmq's in-memory Topic.
package topics

import (
	"sync"
	"sync/atomic"

	"github.com/goptics/varmq/internal/linkedbuffer"
)

const (
	initialChunkCapacity = 1024       // items in the first chunk
	maxChunkCapacity     = 100 * 1024 // items per chunk, cap for growth
)

// Log is an append-only, in-memory log stored in linked chunks, the same
// chunks the queue uses. Each Cursor reads at its own position. The log keeps
// only the write chunk; chunks link forward, so a chunk stays reachable only
// while a cursor sits on it or on an earlier one. Once the slowest cursor
// moves past a chunk nothing references it and the garbage collector frees
// it. Appending never copies: a full chunk links a new one.
//
// ponytail: Len is lock-free, but Append, Dequeue, Purge, Close and Values
// still serialise on one mutex. Upgrade path: per-cursor atomic positions
// with the lock taken only to link a new chunk.
type Log[T any] struct {
	mx      sync.Mutex
	tail    *linkedbuffer.Chunk[T] // write chunk; nil while no cursor is open
	cursors int
	head    uint64        // items ever appended, so the offset of the next one
	headA   atomic.Uint64 // mirror of head, stored under mx, read by Len
}

// Cursor reads a Log from its own position.
type Cursor[T any] struct {
	log    *Log[T]
	chunk  *linkedbuffer.Chunk[T] // holds the next unread item; nil once closed
	idx    int                    // index into chunk.Data
	offset uint64                 // absolute position, for Len
	closed bool

	// Mirrors of offset and closed, stored under log.mx, read lock-free by Len.
	offA    atomic.Uint64
	closedA atomic.Bool
}

// NewLog returns an empty log.
func NewLog[T any]() *Log[T] {
	return &Log[T]{}
}

// Append stores item once and returns how many open cursors will read it.
// With no open cursors nothing is stored and it returns 0.
func (l *Log[T]) Append(item T) int {
	l.mx.Lock()
	defer l.mx.Unlock()

	if l.cursors == 0 {
		return 0
	}

	if !l.tail.Push(item) {
		l.tail = l.tail.Grow(maxChunkCapacity)
		l.tail.Push(item)
	}

	l.head++
	l.headA.Store(l.head)

	return l.cursors
}

// NewCursor returns a cursor positioned at the head: it reads only items
// appended after this call.
func (l *Log[T]) NewCursor() *Cursor[T] {
	l.mx.Lock()
	defer l.mx.Unlock()

	if l.tail == nil {
		l.tail = linkedbuffer.NewChunk[T](initialChunkCapacity)
	}

	c := &Cursor[T]{log: l, chunk: l.tail, idx: l.tail.NextWriteIndex, offset: l.head}
	c.offA.Store(c.offset)
	l.cursors++

	return c
}

// Len returns how many items the cursor has not read. It is 0 once closed. It
// does not take the log's mutex.
func (c *Cursor[T]) Len() int {
	if c.closedA.Load() {
		return 0
	}

	return int(c.log.headA.Load() - c.offA.Load())
}

// Dequeue returns the next unread item and advances the cursor. Stepping off
// a fully read chunk drops this cursor's reference to it.
func (c *Cursor[T]) Dequeue() (T, bool) {
	c.log.mx.Lock()
	defer c.log.mx.Unlock()

	var zero T

	if c.closed {
		return zero, false
	}

	for c.idx == c.chunk.NextWriteIndex {
		if c.chunk.Next == nil {
			return zero, false
		}

		c.chunk = c.chunk.Next
		c.idx = 0
	}

	item := c.chunk.Data[c.idx]
	c.idx++
	c.offset++
	c.offA.Store(c.offset)

	return item, true
}

// Values returns a copy of the unread items without consuming them.
func (c *Cursor[T]) Values() []T {
	c.log.mx.Lock()
	defer c.log.mx.Unlock()

	if c.closed {
		return nil
	}

	out := make([]T, 0, int(c.log.head-c.offset))
	for ch, i := c.chunk, c.idx; ch != nil; ch, i = ch.Next, 0 {
		out = append(out, ch.Data[i:ch.NextWriteIndex]...)
	}

	return out
}

// Purge skips every unread item.
func (c *Cursor[T]) Purge() {
	c.log.mx.Lock()
	defer c.log.mx.Unlock()

	if c.closed {
		return
	}

	c.chunk = c.log.tail
	c.idx = c.log.tail.NextWriteIndex
	c.offset = c.log.head
	c.offA.Store(c.offset)
}

// Close detaches the cursor so it no longer holds chunks. When it was the
// last open cursor the log drops its write chunk too. It is idempotent.
func (c *Cursor[T]) Close() {
	c.log.mx.Lock()
	defer c.log.mx.Unlock()

	if c.closed {
		return
	}

	c.closed = true
	c.closedA.Store(true)
	c.chunk = nil
	c.log.cursors--

	if c.log.cursors == 0 {
		c.log.tail = nil
	}
}
