package queues

import "github.com/goptics/varmq/internal/topics"

// Queue is a FIFO queue: an append-only log in fixed-size linked chunks read
// by one consuming cursor, the same log a topic uses. Producers serialise only
// with each other, readers only with each other, so an Enqueue never waits
// for a Dequeue. The cursor clears each slot as it reads it, so a dequeued
// item is freed at once; a fully read chunk is garbage collected.
type Queue[T any] struct {
	log *topics.Log[T]
	cur *topics.Cursor[T]
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

// Dequeue removes and returns the front item.
func (q *Queue[T]) Dequeue() (any, bool) {
	return q.cur.Dequeue()
}

// Requeue puts items a consumer dequeued but did not use back at the front,
// in order, unless stale reports under the queue's lock that they were
// purged meanwhile: then nothing is stored and it returns false.
func (q *Queue[T]) Requeue(items []any, stale func() bool) bool {
	typed := make([]T, 0, len(items))
	for _, item := range items {
		if v, ok := item.(T); ok {
			typed = append(typed, v)
		}
	}

	return q.cur.Requeue(typed, stale)
}

// Values returns a copy of the items in the queue, front first.
func (q *Queue[T]) Values() []any {
	vals := q.cur.Values()
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v
	}

	return out
}

// Purge removes every item.
func (q *Queue[T]) Purge() {
	q.cur.Purge()
}

// Close makes every later Enqueue return false. Stored items can still be
// dequeued.
func (q *Queue[T]) Close() error {
	q.log.Close()
	return nil
}
