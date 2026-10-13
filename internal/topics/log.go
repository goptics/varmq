// Package topics implements an in-memory append-only log read by independent
// cursors. It is the storage behind varmq's in-memory Topic.
package topics

import (
	"slices"
	"sync"
)

// Log is an append-only, in-memory log. Each Cursor reads it at its own
// offset. Items every open cursor has read are dropped the next time the log
// would grow.
//
// ponytail: one mutex guards everything, so every Dequeue from every cursor
// serialises on it, and the backing array never shrinks after a burst.
// Upgrade path: per-cursor atomic offsets with the lock taken only to trim.
type Log[T any] struct {
	mx      sync.Mutex
	items   []T
	base    uint64 // offset of items[0]
	cursors map[*Cursor[T]]struct{}
}

// Cursor reads a Log from its own offset.
type Cursor[T any] struct {
	log    *Log[T]
	offset uint64
	closed bool
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
		l.trim()
		return 0
	}

	if len(l.items) == cap(l.items) {
		l.trim()
	}

	l.items = append(l.items, item)

	return n
}

// trim drops every item below the lowest open cursor offset and zeroes the
// freed slots so the GC can reclaim what they pointed to. Caller holds l.mx.
func (l *Log[T]) trim() {
	low := l.head()
	for c := range l.cursors {
		low = min(low, c.offset)
	}

	drop := int(low - l.base)
	if drop == 0 {
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
	l.cursors[c] = struct{}{}

	return c
}

// Len returns how many items the cursor has not read. It is 0 once closed.
func (c *Cursor[T]) Len() int {
	c.log.mx.Lock()
	defer c.log.mx.Unlock()

	if c.closed {
		return 0
	}

	return int(c.log.head() - c.offset)
}

// Dequeue returns the next unread item and advances the cursor.
func (c *Cursor[T]) Dequeue() (T, bool) {
	c.log.mx.Lock()
	defer c.log.mx.Unlock()

	if c.closed || c.offset == c.log.head() {
		var zero T
		return zero, false
	}

	item := c.log.items[c.offset-c.log.base]
	c.offset++

	return item, true
}

// Values returns a copy of the unread items without consuming them.
func (c *Cursor[T]) Values() []T {
	c.log.mx.Lock()
	defer c.log.mx.Unlock()

	if c.closed {
		return nil
	}

	return slices.Clone(c.log.items[c.offset-c.log.base:])
}

// Purge skips every unread item.
func (c *Cursor[T]) Purge() {
	c.log.mx.Lock()
	defer c.log.mx.Unlock()

	if !c.closed {
		c.offset = c.log.head()
	}
}

// Close detaches the cursor so it no longer holds items in the log. It is
// idempotent.
func (c *Cursor[T]) Close() {
	c.log.mx.Lock()
	defer c.log.mx.Unlock()

	if c.closed {
		return
	}

	c.closed = true
	delete(c.log.cursors, c)
}
