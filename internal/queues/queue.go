package queues

import (
	"sync"
	"sync/atomic"

	"github.com/goptics/varmq/internal/topics"
)

// Queue is a FIFO queue: an append-only log in fixed-size linked chunks read
// by one consuming cursor, the same log a topic uses. Producers serialise only
// with each other, readers only with each other, so an Enqueue never waits
// for a Dequeue. The cursor clears each slot as it reads it, so a dequeued
// item is freed at once; a fully read chunk is garbage collected.
//
// EnqueueOnce adds an id set, striped over idShards locks, which the Dequeue
// of an item stored with an id also locks; plain Enqueue and Dequeue never
// touch it.
type Queue[T any] struct {
	log *topics.Log[T]
	cur *topics.Cursor[T]

	attachOnce sync.Once
	attached   any

	// Every Enqueue reads log. Padded to 128 bytes, so that read never shares
	// a line with the cursor, allocated just before and written on every
	// Dequeue.
	_ [128 - 8 - 8 - 16 - 16]byte

	ids   atomic.Pointer[idSet] // ids of stored items EnqueueOnce added; nil until the first
	idsOn atomic.Bool           // set by the first EnqueueOnce, before its Append
}

// identified is an item that carries the id EnqueueOnce stored it under.
type identified interface {
	ID() string
}

// Attach returns the value kept with this queue, made by make on the first
// call and shared by everyone holding the queue. varmq keeps the Purge state
// of the workers bound to it here.
func (q *Queue[T]) Attach(make func() any) any {
	q.attachOnce.Do(func() { q.attached = make() })

	return q.attached
}

// NewQueue creates a new empty queue.
func NewQueue[T any]() *Queue[T] {
	l := topics.NewLog[T]()

	return &Queue[T]{log: l, cur: l.NewConsumer()}
}

// SetCapacity bounds how many items the queue holds; Enqueue returns false
// while it is full. Values below 1 mean unlimited.
func (q *Queue[T]) SetCapacity(cap int) {
	q.log.SetLimit(cap)
}

// Len returns the number of items in the queue.
func (q *Queue[T]) Len() int {
	return q.cur.Len()
}

// Enqueue adds an item to the back of the queue. It returns false when the
// item is not a T, the queue is full or it is closed.
func (q *Queue[T]) Enqueue(item any) bool {
	typedItem, ok := item.(T)
	if !ok {
		return false
	}

	return q.log.Append(typedItem) > 0
}

// EnqueueOnce adds item, which must return id from its ID method, unless an
// item with the same id is in the queue. It returns false for a repeat and
// where Enqueue would. The id is free again once Dequeue or Purge takes the
// item.
//
// It holds only the lock of id's shard across the Append, so adds of
// different ids rarely wait on each other or on a Dequeue.
func (q *Queue[T]) EnqueueOnce(item any, id string) bool {
	typedItem, ok := item.(T)
	if !ok {
		return false
	}

	sh := q.idSet().shard(id)
	if !q.idsOn.Load() { // a store on every add would bounce the line between cores
		q.idsOn.Store(true)
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	if !sh.ids.add(id) {
		return false
	}

	if q.log.Append(typedItem) == 0 {
		sh.ids.remove(id)
		return false
	}

	return true
}

// idSet returns the id set, making it on the first call.
func (q *Queue[T]) idSet() *idSet {
	if s := q.ids.Load(); s != nil {
		return s
	}

	q.ids.CompareAndSwap(nil, newIdSet())

	return q.ids.Load()
}

// Dequeue removes and returns the front item, freeing its id if
// EnqueueOnce stored it.
func (q *Queue[T]) Dequeue() (any, bool) {
	v, ok := q.cur.Dequeue()

	// Read after the dequeue: EnqueueOnce sets idsOn before its Append, so an
	// item stored with an id always finds it set, and its id is in by the
	// time the shard lock is free.
	if ok && q.idsOn.Load() {
		if j, isId := any(v).(identified); isId {
			if id := j.ID(); id != "" {
				q.ids.Load().remove(id)
			}
		}
	}

	return v, ok
}

// Values returns a copy of the items in the queue, front first.
func (q *Queue[T]) Values() []any {
	return anys(q.cur.Values())
}

// Purge removes every item and frees their ids. It exists for IBaseQueue,
// whose Purge returns nothing; varmq purges through PurgeValues
// (IPurgeValuesQueue).
func (q *Queue[T]) Purge() {
	q.PurgeValues()
}

// PurgeValues removes every item, frees their ids and returns the removed
// items, front first. It is one step, so an Enqueue racing it is either
// returned or stays queued, never removed unseen. Holding idMu keeps an
// EnqueueOnce from landing between the purge and the id reset.
// ponytail: a Dequeue that took an item before Purge and frees its id after a
// re-add of that id frees the new one, letting one repeat in; no id leaks.
func (q *Queue[T]) PurgeValues() []any {
	q.idMu.Lock()
	defer q.idMu.Unlock()

	vals := q.cur.Purge()
	q.ids, q.idsPeak = nil, 0

	return anys(vals)
}

// anys copies vals into the []any that IBaseQueue hands out.
func anys[T any](vals []T) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v
	}

	return out
// Purge removes every item and frees their ids. Holding every shard lock
// keeps an EnqueueOnce from landing between the two.
// ponytail: a Dequeue that took an item before Purge and frees its id after a
// re-add of that id frees the new one, letting one repeat in; no id leaks.
// Purge makes the set even on a queue that never saw an id (4 KiB), so a
// racing first EnqueueOnce shares its locks.
func (q *Queue[T]) Purge() {
	ids := q.idSet()
	for i := range ids.shards {
		ids.shards[i].mu.Lock()
	}

	q.cur.Purge()

	for i := range ids.shards {
		ids.shards[i].ids = idMap{}
		ids.shards[i].mu.Unlock()
	}
}

// Close makes every later Enqueue return false. Stored items can still be
// dequeued.
func (q *Queue[T]) Close() error {
	q.log.Close()
	return nil
}
