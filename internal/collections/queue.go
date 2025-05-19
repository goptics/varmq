package collections

import "sync"

type Queue[T any] struct {
	elements []T // Slice to store queue elements
	front    int // Index of the front element
	size     int // Current number of elements in the queue
	mx       sync.RWMutex
}

// NewQueue creates a new empty queue with slice-based implementation
func NewQueue[T any]() *Queue[T] {
	return &Queue[T]{
		elements: make([]T, 100), // Start with capacity of 100
		front:    0,
		size:     0,
	}
}

// Values returns a slice of all values in the queue
func (q *Queue[T]) Values() []any {
	q.mx.RLock()
	defer q.mx.RUnlock()

	values := make([]any, 0, q.size)

	// Copy all elements from front to rear, handling wrap-around
	for i := range q.size {
		index := (q.front + i) % len(q.elements)
		values = append(values, q.elements[index])
	}

	return values
}

// Len returns the number of items in the queue
func (q *Queue[T]) Len() int {
	q.mx.RLock()
	defer q.mx.RUnlock()
	return q.size
}

// Enqueue adds an item to the back of the queue
// Time complexity: O(1) amortized due to occasional resizing
func (q *Queue[T]) Enqueue(item any) bool {
	q.mx.Lock()
	defer q.mx.Unlock()

	// Check if we need to resize
	if q.size == len(q.elements) {
		q.resize(len(q.elements) * 2)
	}

	// Calculate the index to insert at
	rear := (q.front + q.size) % len(q.elements)

	// Insert the item and update size
	q.elements[rear] = item.(T)
	q.size++

	return true
}

// Dequeue removes and returns the front item
// Time complexity: O(1)
func (q *Queue[T]) Dequeue() (any, bool) {
	q.mx.Lock()
	defer q.mx.Unlock()
	var zeroValue T

	// Check if queue is empty
	if q.size == 0 {
		return zeroValue, false
	}

	// Get the front item
	item := q.elements[q.front]

	// Clear reference to help garbage collection
	q.elements[q.front] = zeroValue

	// Update front and size
	q.front = (q.front + 1) % len(q.elements)
	q.size--

	// We don't automatically shrink the queue to avoid unnecessary allocations
	// Go's GC will handle memory management

	return item, true
}

// resize changes the capacity of the queue while preserving the order of elements
// This is a helper function used internally
func (q *Queue[T]) resize(newCapacity int) {
	newElements := make([]T, newCapacity)

	// Copy elements from the old slice to the new one, handling wrap-around
	for i := range q.size {
		newElements[i] = q.elements[(q.front+i)%len(q.elements)]
	}

	// Update the queue properties
	q.elements = newElements
	q.front = 0
}

// Purge clears all elements from the queue
func (q *Queue[T]) Purge() {
	q.mx.Lock()
	defer q.mx.Unlock()

	// Reset to a clean state with small capacity
	q.elements = make([]T, 100)
	q.front = 0
	q.size = 0
}

// Close releases resources and clears the queue
// This is mainly for interface compatibility
func (q *Queue[T]) Close() error {
	q.Purge()
	return nil
}
