package queues

import (
	"sync"
	"sync/atomic"
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
	capacity       int // Maximum size of the queue
	mx             sync.RWMutex
	closed         atomic.Bool
}

// newPriorityQueue initializes an empty priority queue.
func NewPriorityQueue[T any]() *PriorityQueue[T] {
	return &PriorityQueue[T]{internal: &heapQueue[T]{}}
}

func (q *PriorityQueue[T]) SetCapacity(cap int) {
	q.capacity = max(0, cap)
}

// Len returns the number of items in the priority queue.
func (q *PriorityQueue[T]) Len() int {
	q.mx.RLock()
	defer q.mx.RUnlock()
	return q.internal.Len()
}

// Values returns a slice of all values in the priority queue.
func (q *PriorityQueue[T]) Values() []any {
	q.mx.RLock()
	defer q.mx.RUnlock()
	values := make([]any, 0, len(q.internal.items))
	for i := range q.internal.items {
		values = append(values, q.internal.items[i].Value)
	}
	return values
}

// Enqueue pushes a new item with the given priority.
// Time complexity: O(log n)
func (q *PriorityQueue[T]) Enqueue(item any, priority int) bool {
	typedValue, ok := item.(T)

	if !ok {
		return false
	}

	q.mx.Lock()

	// closed is checked under the lock so a Close followed by Purge cannot
	// interleave with an in-flight Enqueue (see Subscription.Unsubscribe).
	if q.closed.Load() {
		q.mx.Unlock()
		return false
	}

	if q.capacity > 0 && q.internal.Len() == q.capacity {
		q.mx.Unlock()
		return false
	}

	q.internal.push(enqItem[T]{
		Value:    typedValue,
		Priority: priority,
		Index:    q.insertionCount,
	}) // O(log n)
	q.insertionCount++
	q.mx.Unlock()

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
	return q.internal.pop().Value, true // O(log n)
}

func (q *PriorityQueue[T]) Purge() {
	q.mx.Lock()
	defer q.mx.Unlock()
	// Drop the backing array rather than reslicing to 0: it holds the purged
	// values and, at 32 B a slot, would pin the queue's high-water memory.
	q.internal.items = nil
}

// to satisfy the IQueue interface
func (q *PriorityQueue[T]) Close() error {
	q.closed.Store(true)
	return nil
}
