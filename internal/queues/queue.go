package queues

import (
	"math"
	"sync"
	"sync/atomic"

	"github.com/goptics/varmq/internal/linkedbuffer"
)

var (
	initialBufferCapacity = 1024       // 1KB initial capacity
	chunkMaxCapacity      = 100 * 1024 // 100KB max capacity
)

// Queue implements a FIFO queue using linked buffer chunks
// This eliminates large slice allocations while maintaining good cache locality
// Inspired by [pond](https://github.com/alitto/pond) buffer implementation with dynamic capacity growth
type Queue[T any] struct {
	readChunk   *linkedbuffer.Chunk[T] // Current chunk being read from
	writeChunk  *linkedbuffer.Chunk[T] // Current chunk being written to
	writeCount  atomic.Uint64          // Total items written
	readCount   atomic.Uint64          // Total items read
	mx          sync.RWMutex           // Protects chunk pointers
	maxCapacity int                    // Maximum capacity per chunk
	closed      atomic.Bool
}

// NewQueue creates a new empty linked buffer queue
func NewQueue[T any]() *Queue[T] {
	chunk := linkedbuffer.NewChunk[T](initialBufferCapacity)

	return &Queue[T]{
		readChunk:   chunk,
		writeChunk:  chunk,
		maxCapacity: chunkMaxCapacity,
	}
}

// Len returns the total number of items in the queue
func (q *Queue[T]) Len() int {
	writeCount := q.writeCount.Load()
	readCount := q.readCount.Load()

	if writeCount < readCount {
		// The writeCount counter wrapped around
		return int(math.MaxUint64 - readCount + writeCount)
	}

	return int(writeCount - readCount)
}

// Enqueue adds an item to the back of the queue
// Time complexity: O(1) amortized - only allocates new chunks when needed
func (q *Queue[T]) Enqueue(item any) bool {
	if q.closed.Load() {
		return false
	}

	typedItem, ok := item.(T)

	if !ok {
		return false
	}

	q.mx.Lock()
	defer q.mx.Unlock()

	// Try to push to current write chunk
	if q.writeChunk.Push(typedItem) {
		q.writeCount.Add(1)
		return true
	}

	currentCap := q.writeChunk.Cap()
	newCapacity := min(currentCap+currentCap/2, q.maxCapacity)

	newChunk := linkedbuffer.NewChunk[T](newCapacity)

	q.writeChunk.Next = newChunk
	q.writeChunk = newChunk

	if q.writeChunk.Push(typedItem) {
		q.writeCount.Add(1)
		return true
	}

	return false // Should never happen
}

// Dequeue removes and returns the front item
// Time complexity: O(1) amortized - may advance to next chunk
func (q *Queue[T]) Dequeue() (any, bool) {
	q.mx.Lock()
	defer q.mx.Unlock()

	// Try to pop from current read chunk
	if item, ok := q.readChunk.Pop(); ok {
		q.readCount.Add(1)
		return item, true
	}

	// Current chunk is empty, try to move to next chunk
	if q.readChunk.Next != nil {
		q.readChunk = q.readChunk.Next

		// Try again with new chunk
		if item, ok := q.readChunk.Pop(); ok {
			q.readCount.Add(1)
			return item, true
		}
	}

	// No items available
	return *new(T), false
}

// Values returns a slice of all values in the queue
// Note: This creates a temporary slice for compatibility
func (q *Queue[T]) Values() []any {
	q.mx.RLock()
	defer q.mx.RUnlock()

	length := q.Len()
	if length == 0 {
		return nil
	}

	values := make([]any, 0, length)

	// Iterate through all chunks starting from read chunk
	for chunk := q.readChunk; chunk != nil; chunk = chunk.Next {
		// Add unread items from this chunk
		for i := chunk.NextReadIndex; i < chunk.NextWriteIndex; i++ {
			values = append(values, chunk.Data[i])
		}
	}

	return values
}

// Purge clears all elements from the queue
func (q *Queue[T]) Purge() {
	q.mx.Lock()
	defer q.mx.Unlock()

	// Reset to single chunk with initial capacity
	chunk := linkedbuffer.NewChunk[T](initialBufferCapacity)
	q.readChunk = chunk
	q.writeChunk = chunk
	q.readCount.Store(0)
	q.writeCount.Store(0)
}

// Close releases resources and clears the queue
func (q *Queue[T]) Close() error {
	q.closed.Store(true)
	return nil
}
