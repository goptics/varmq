package linkedbuffer

// Chunk represents a dynamic-size buffer inspired by pond's approach
type Chunk[T any] struct {
	Data           []T       // Dynamic slice that grows as needed
	NextWriteIndex int       // Index for next write
	NextReadIndex  int       // Index for next read
	Next           *Chunk[T] // Next chunk in the linked list
}

// NewChunk creates a new buffer chunk with initial capacity
func NewChunk[T any](capacity int) *Chunk[T] {
	return &Chunk[T]{
		Data: make([]T, capacity),
	}
}

func (c *Chunk[T]) Len() int {
	return c.NextWriteIndex - c.NextReadIndex
}

func (c *Chunk[T]) Cap() int {
	return cap(c.Data)
}

// IsFull returns true if the chunk cannot accept more items
func (c *Chunk[T]) IsFull() bool {
	return c.NextWriteIndex >= c.Cap()
}

// IsEmpty returns true if the chunk has no items to read
func (c *Chunk[T]) IsEmpty() bool {
	return c.NextReadIndex >= c.NextWriteIndex
}

// Push adds an item to the chunk
// Returns false if the chunk is full
func (c *Chunk[T]) Push(item T) bool {
	if c.IsFull() {
		return false
	}

	c.Data[c.NextWriteIndex] = item
	c.NextWriteIndex++
	return true
}

// Pop removes and returns an item from the front of the chunk
// Returns zero value and false if the chunk is empty
func (c *Chunk[T]) Pop() (T, bool) {
	if c.IsEmpty() {
		return *new(T), false
	}

	item := c.Data[c.NextReadIndex]

	// Clear reference to prevent memory leaks
	c.Data[c.NextReadIndex] = *new(T)
	c.NextReadIndex++

	return item, true
}
