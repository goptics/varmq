// Package topics implements an in-memory append-only log read by independent
// cursors. It is the storage behind varmq's in-memory Topic.
package topics

import (
	"sync"
	"sync/atomic"
)

// slot is one log entry plus how many open cursors still have to read it.
type slot[T any] struct {
	v    T
	left int // open cursors that have not read this slot yet
}

// Log is an append-only, in-memory log. Each Cursor reads it at its own
// offset. A payload is released as soon as every open cursor has read, purged,
// or closed past it, and the log's memory is released entirely when its last
// cursor closes. trim only compacts the backing array: it drops the slots
// below the lowest open offset when that frees at least half of the log.
//
// ponytail: Len is lock-free, but Append, Dequeue, Purge, Close and Values
// still serialise on one mutex, and the backing array never shrinks after a
// burst. Upgrade path: per-cursor atomic offsets with the lock taken only to
// trim.
type Log[T any] struct {
	mx      sync.Mutex
	items   []slot[T]
	base    uint64 // offset of items[0]
	cursors map[*Cursor[T]]struct{}
	headA   atomic.Uint64 // mirror of head(), stored under mx, read by Len
}

// Cursor reads a Log from its own offset.
type Cursor[T any] struct {
	log    *Log[T]
	offset uint64
	closed bool

	// Mirrors of offset and closed, stored under log.mx, read lock-free by Len.
	offA    atomic.Uint64
	closedA atomic.Bool
}

// NewLog returns an empty log.
func NewLog[T any]() *Log[T] {
	return &Log[T]{cursors: make(map[*Cursor[T]]struct{})}
}

func (l *Log[T]) head() uint64 {
	return l.base + uint64(len(l.items))
}

// Append stores item once and returns how many open cursors will read it.
// With no open cursors nothing is stored and it returns 0.
func (l *Log[T]) Append(item T) int {
	l.mx.Lock()
	defer l.mx.Unlock()

	n := len(l.cursors)
	if n == 0 {
		// No readers: nothing is kept, so trim empties the log.
		l.trim()
		return 0
	}

	if len(l.items) == cap(l.items) {
		l.trim()
	}

	l.items = append(l.items, slot[T]{v: item, left: n})
	l.headA.Store(l.head())

	return n
}

// trim drops every slot below the lowest open cursor offset and zeroes the
// freed tail of the backing array. It only compacts when that frees at least
// half of the log, so a backlog near capacity does not copy on every append;
// otherwise it changes nothing and the caller grows the slice. Payloads are
// already released by the cursors; trim only reclaims the slots. Caller holds
// l.mx.
func (l *Log[T]) trim() {
	low := l.head()
	for c := range l.cursors {
		low = min(low, c.offset)
	}

	drop := int(low - l.base)
	if drop == 0 || drop < len(l.items)/2 {
		return
	}

	n := copy(l.items, l.items[drop:])
	clear(l.items[n:])
	l.items = l.items[:n]
	l.base = low
}

// NewCursor returns a cursor positioned at the head: it reads only items
// appended after this call.
func (l *Log[T]) NewCursor() *Cursor[T] {
	l.mx.Lock()
	defer l.mx.Unlock()

	c := &Cursor[T]{log: l, offset: l.head()}
	c.offA.Store(c.offset)
	l.cursors[c] = struct{}{}

	return c
}

// release marks one slot as read by one cursor and drops its payload once no
// open cursor still needs it. Caller holds l.mx.
func (s *slot[T]) release() {
	s.left--
	if s.left <= 0 {
		var zero T
		s.v = zero
	}
}

// skip releases every slot in [offset, head) for this cursor. Caller holds
// l.mx.
func (c *Cursor[T]) skip() {
	l := c.log
	for o := c.offset; o < l.head(); o++ {
		l.items[o-l.base].release()
	}
}

// Len returns how many items the cursor has not read. It is 0 once closed. It
// does not take the log's mutex.
func (c *Cursor[T]) Len() int {
	if c.closedA.Load() {
		return 0
	}

	// The offset is loaded first so a concurrent Dequeue cannot make it exceed
	// the head loaded after it.
	o := c.offA.Load()
	h := c.log.headA.Load()
	if h > o {
		return int(h - o)
	}

	return 0
}

// Dequeue returns the next unread item and advances the cursor.
func (c *Cursor[T]) Dequeue() (T, bool) {
	c.log.mx.Lock()
	defer c.log.mx.Unlock()

	if c.closed || c.offset == c.log.head() {
		var zero T
		return zero, false
	}

	s := &c.log.items[c.offset-c.log.base]
	item := s.v
	s.release()
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

	unread := c.log.items[c.offset-c.log.base:]
	out := make([]T, len(unread))
	for i := range unread {
		out[i] = unread[i].v
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

	c.skip()
	c.offset = c.log.head()
	c.offA.Store(c.offset)
}

// Close detaches the cursor so it no longer holds items in the log. When it
// was the last open cursor the log releases its memory. It is idempotent.
func (c *Cursor[T]) Close() {
	c.log.mx.Lock()
	defer c.log.mx.Unlock()

	if c.closed {
		return
	}

	c.skip()
	c.closed = true
	c.closedA.Store(true)
	delete(c.log.cursors, c)

	if len(c.log.cursors) == 0 {
		c.log.base = c.log.head()
		c.log.items = nil
	}
}
