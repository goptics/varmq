package collections

import (
	"container/list"
	"sync"
)

// Queue holds a linked list-based queue of generic items.
type Queue[T any] struct {
	internal *list.List
	mx       sync.Mutex
}

// newQueue creates an empty Queue using container/list.
func NewQueue[T any]() *Queue[T] {
	return &Queue[T]{internal: new(list.List)}
}

// Values returns a slice of all values in the queue.
func (q *Queue[T]) Values() []any {
	q.mx.Lock()
	defer q.mx.Unlock()
	values := make([]any, 0)

	for e := q.internal.Front(); e != nil; e = e.Next() {
		values = append(values, e.Value)
	}

	return values
}

// Len returns the number of items in the queue.
func (q *Queue[T]) Len() int {
	q.mx.Lock()
	defer q.mx.Unlock()
	return q.internal.Len()
}

// Enqueue adds an item at the back of the list.
// Time complexity: O(1)
func (q *Queue[T]) Enqueue(item any) bool {
	q.mx.Lock()
	defer q.mx.Unlock()
	q.internal.PushBack(item)
	return true
}

// Dequeue removes and returns the front item.
// Time complexity: O(1)
func (q *Queue[T]) Dequeue() (any, bool) {
	q.mx.Lock()
	defer q.mx.Unlock()
	front := q.internal.Front()
	if front == nil {
		// Return zero value + false if empty
		var zeroValue T
		return zeroValue, false
	}

	// Retrieve the item’s value and remove the element in O(1).
	val := front.Value.(T)
	q.internal.Remove(front)
	return val, true
}

func (q *Queue[T]) Purge() {
	q.mx.Lock()
	defer q.mx.Unlock()
	q.internal.Init()
}

func (q *Queue[T]) Close() error {
	q.Purge()
	return nil
}
