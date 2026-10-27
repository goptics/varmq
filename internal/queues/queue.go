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
// EnqueueOnce adds one more lock, idMu, shared with the Dequeue of an item
// stored with an id; plain Enqueue and Dequeue of items without one never
// take it.
type Queue[T any] struct {
	log *topics.Log[T]
	cur *topics.Cursor[T]

	attachOnce sync.Once
	attached   any

	// Every Enqueue reads log. Padded to 128 bytes, so that read never shares
	// a line with the cursor, allocated just before and written on every
	// Dequeue. The id set sits after this hot 128-byte block, so
	// EnqueueOnce's writes to idMu stay out of the block's 128-byte prefetch
	// pair. The struct is 192 bytes, a size class of its own.
	_ [128 - 8 - 8 - 16 - 16]byte

	ids     map[string]struct{} // ids of stored items EnqueueOnce added; under idMu
	idsPeak int                 // most ids held since ids was made; under idMu
	idMu    sync.Mutex
	idsOn   atomic.Bool // set by the first EnqueueOnce, before its Append

	_ [64 - 8 - 8 - 8 - 4]byte
}

// identified is an item that carries the id EnqueueOnce stored it under.
type identified interface {
	ID() string
}

// idsShrinkPeak is the set size above which a drained set is dropped. A map
// keeps its buckets after its keys are deleted, so a burst of ids would hold
// its peak memory; a small set is kept, sparing an allocation per refill.
const idsShrinkPeak = 1024

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
func (q *Queue[T]) EnqueueOnce(item any, id string) bool {
	typedItem, ok := item.(T)
	if !ok {
		return false
	}

	q.idMu.Lock()
	defer q.idMu.Unlock()

	if q.ids == nil {
		q.ids = make(map[string]struct{})
		q.idsOn.Store(true)
	}

	if _, dup := q.ids[id]; dup {
		return false
	}

	if q.log.Append(typedItem) == 0 {
		return false
	}

	q.ids[id] = struct{}{}
	q.idsPeak = max(q.idsPeak, len(q.ids))

	return true
}

// Dequeue removes and returns the front item, freeing its id if
// EnqueueOnce stored it.
func (q *Queue[T]) Dequeue() (any, bool) {
	v, ok := q.cur.Dequeue()

	// Read after the dequeue: EnqueueOnce sets idsOn before its Append, so an
	// item stored with an id always finds it set. An item dequeued between
	// that Append and the insert waits on idMu, so the delete comes after.
	if ok && q.idsOn.Load() {
		q.release(v)
	}

	return v, ok
}

func (q *Queue[T]) release(v T) {
	j, ok := any(v).(identified)
	if !ok {
		return
	}

	if id := j.ID(); id != "" {
		q.idMu.Lock()
		delete(q.ids, id)
		if len(q.ids) == 0 && q.idsPeak > idsShrinkPeak {
			q.ids, q.idsPeak = nil, 0
		}
		q.idMu.Unlock()
	}
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
}

// Close makes every later Enqueue return false. Stored items can still be
// dequeued.
func (q *Queue[T]) Close() error {
	q.log.Close()
	return nil
}
