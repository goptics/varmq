package queues

import (
	"container/heap"
	"sync"
)

type enqItem[T any] struct {
	Value    T
	Priority int
	Index    int
}

// PriorityQueue is the user-facing wrapper around heapQueue[T].
type PriorityQueue[T any] struct {
	internal       *heapQueue[T]
	insertionCount int
	mx             sync.Mutex
}

// newPriorityQueue initializes an empty priority queue.
func NewPriorityQueue[T any]() *PriorityQueue[T] {
	pq := &heapQueue[T]{
		items: make([]*enqItem[T], 0),
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
func (q *PriorityQueue[T]) Enqueue(item any, priority int) bool {
	q.mx.Lock()
	defer q.mx.Unlock()

	typedValue, ok := item.(T)

	if !ok {
		return false
	}

	i := enqItem[T]{
		Value:    typedValue,
		Priority: priority,
		Index:    q.insertionCount,
	}

	q.insertionCount++
	heap.Push(q.internal, &i) // O(log n)
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
	popped := heap.Pop(q.internal).(*enqItem[T]) // O(log n)
	return popped.Value, true
}

func (q *PriorityQueue[T]) Purge() {
	q.mx.Lock()
	defer q.mx.Unlock()
	q.internal.items = make([]*enqItem[T], 0)
	heap.Init(q.internal)
}

// to satisfy the IQueue interface
func (q *PriorityQueue[T]) Close() error {
	q.Purge()
	return nil
}
