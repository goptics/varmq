package queue

import (
	"container/heap"
	"sync"
)

// PriorityQueue is the user-facing wrapper around heapQueue[T].
type PriorityQueue[T any] struct {
	internal       *heapQueue[T]
	insertionCount int
	mx             sync.Mutex
}

// newPriorityQueue initializes an empty priority queue.
func NewPriorityQueue[T any]() *PriorityQueue[T] {
	pq := &heapQueue[T]{
		items: make([]*EnqItem[T], 0),
	}
	heap.Init(pq)
	return &PriorityQueue[T]{internal: pq}
}

// Len returns the number of items in the priority queue.
func (q *PriorityQueue[T]) Len() int {
	q.mx.Lock()
	defer q.mx.Unlock()
	return q.internal.Len()
}

// Values returns a slice of all values in the priority queue.
func (q *PriorityQueue[T]) Values() []any {
	q.mx.Lock()
	defer q.mx.Unlock()
	values := make([]any, 0)
	for _, item := range q.internal.items {
		values = append(values, item.Value)
	}
	return values
}

// Enqueue pushes a new item with the given priority.
// Time complexity: O(log n)
func (q *PriorityQueue[T]) Enqueue(item any) bool {
	q.mx.Lock()
	defer q.mx.Unlock()
	t, ok := item.(EnqItem[T])
	if !ok {
		return false
	}

	t.Index = q.insertionCount
	q.insertionCount++
	heap.Push(q.internal, &t) // O(log n)
	return true
}

// Dequeue removes and returns the item with the *smallest* Priority.
// Time complexity: O(log n)
func (q *PriorityQueue[T]) Dequeue() (any, bool) {
	q.mx.Lock()
	defer q.mx.Unlock()
	var zeroValue T
	if q.internal.Len() == 0 {
		return zeroValue, false
	}
	popped := heap.Pop(q.internal).(*EnqItem[T]) // O(log n)
	return popped.Value, true
}

// to satisfy the IQueue interface
func (q *PriorityQueue[T]) Close() error {
	q.mx.Lock()
	defer q.mx.Unlock()
	q.internal.items = make([]*EnqItem[T], 0)
	heap.Init(q.internal)
	return nil
}
