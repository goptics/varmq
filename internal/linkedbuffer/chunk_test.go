package linkedbuffer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewChunk(t *testing.T) {
	t.Run("Creates chunk with specified capacity", func(t *testing.T) {
		assert := assert.New(t)
		capacity := 10
		chunk := NewChunk[int](capacity)

		assert.NotNil(chunk, "Chunk should not be nil")
		assert.Equal(capacity, len(chunk.Data), "Data slice should have correct capacity")
		assert.Equal(0, chunk.NextWriteIndex, "NextWriteIndex should be 0")
		assert.Equal(0, chunk.NextReadIndex, "NextReadIndex should be 0")
		assert.Nil(chunk.Next, "Next should be nil")
	})

	t.Run("Creates chunk with zero capacity", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[string](0)

		assert.NotNil(chunk, "Chunk should not be nil")
		assert.Equal(0, len(chunk.Data), "Data slice should have zero capacity")
		assert.Equal(0, chunk.NextWriteIndex, "NextWriteIndex should be 0")
		assert.Equal(0, chunk.NextReadIndex, "NextReadIndex should be 0")
	})

	t.Run("Creates chunk with large capacity", func(t *testing.T) {
		assert := assert.New(t)
		capacity := 1000
		chunk := NewChunk[float64](capacity)

		assert.NotNil(chunk, "Chunk should not be nil")
		assert.Equal(capacity, len(chunk.Data), "Data slice should have correct capacity")
	})
}

func TestChunk_Len(t *testing.T) {
	t.Run("Empty chunk has zero length", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](5)

		assert.Equal(0, chunk.Len(), "Empty chunk should have length 0")
	})

	t.Run("Length increases with push operations", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](5)

		assert.Equal(0, chunk.Len(), "Initial length should be 0")

		chunk.Push(1)
		assert.Equal(1, chunk.Len(), "Length should be 1 after first push")

		chunk.Push(2)
		assert.Equal(2, chunk.Len(), "Length should be 2 after second push")

		chunk.Push(3)
		assert.Equal(3, chunk.Len(), "Length should be 3 after third push")
	})

	t.Run("Length decreases with pop operations", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](5)

		// Fill chunk with items
		chunk.Push(1)
		chunk.Push(2)
		chunk.Push(3)
		assert.Equal(3, chunk.Len(), "Length should be 3 after pushes")

		// Pop items and check length
		chunk.Pop()
		assert.Equal(2, chunk.Len(), "Length should be 2 after first pop")

		chunk.Pop()
		assert.Equal(1, chunk.Len(), "Length should be 1 after second pop")

		chunk.Pop()
		assert.Equal(0, chunk.Len(), "Length should be 0 after third pop")
	})

	t.Run("Length with mixed push and pop operations", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](5)

		// Push some items
		chunk.Push(1)
		chunk.Push(2)
		assert.Equal(2, chunk.Len(), "Length should be 2")

		// Pop one item
		chunk.Pop()
		assert.Equal(1, chunk.Len(), "Length should be 1 after pop")

		// Push more items
		chunk.Push(3)
		chunk.Push(4)
		assert.Equal(3, chunk.Len(), "Length should be 3 after more pushes")

		// Pop remaining items
		chunk.Pop()
		chunk.Pop()
		chunk.Pop()
		assert.Equal(0, chunk.Len(), "Length should be 0 after popping all")
	})

	t.Run("Length with full chunk", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](3)

		// Fill chunk completely
		chunk.Push(1)
		chunk.Push(2)
		chunk.Push(3)
		assert.Equal(3, chunk.Len(), "Full chunk should have correct length")

		// Try to push to full chunk (should fail)
		success := chunk.Push(4)
		assert.False(success, "Push to full chunk should fail")
		assert.Equal(3, chunk.Len(), "Length should remain unchanged after failed push")
	})

	t.Run("Length with zero capacity chunk", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](0)

		assert.Equal(0, chunk.Len(), "Zero capacity chunk should have length 0")

		// Try to push (should fail)
		success := chunk.Push(1)
		assert.False(success, "Push to zero capacity chunk should fail")
		assert.Equal(0, chunk.Len(), "Length should remain 0")
	})

	t.Run("Length with different data types", func(t *testing.T) {
		t.Run("String chunk", func(t *testing.T) {
			assert := assert.New(t)
			chunk := NewChunk[string](3)

			chunk.Push("hello")
			chunk.Push("world")
			assert.Equal(2, chunk.Len(), "String chunk should have correct length")
		})

		t.Run("Pointer chunk", func(t *testing.T) {
			assert := assert.New(t)
			chunk := NewChunk[*int](3)
			val1, val2 := 42, 84

			chunk.Push(&val1)
			chunk.Push(&val2)
			assert.Equal(2, chunk.Len(), "Pointer chunk should have correct length")
		})

		t.Run("Struct chunk", func(t *testing.T) {
			type TestStruct struct {
				ID   int
				Name string
			}

			assert := assert.New(t)
			chunk := NewChunk[TestStruct](3)

			chunk.Push(TestStruct{ID: 1, Name: "first"})
			chunk.Push(TestStruct{ID: 2, Name: "second"})
			assert.Equal(2, chunk.Len(), "Struct chunk should have correct length")
		})
	})

	t.Run("Length consistency with IsEmpty and IsFull", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](3)

		// Empty chunk
		assert.Equal(0, chunk.Len(), "Length should be 0")
		assert.True(chunk.IsEmpty(), "Chunk should be empty")
		assert.False(chunk.IsFull(), "Chunk should not be full")

		// Partially filled chunk
		chunk.Push(1)
		chunk.Push(2)
		assert.Equal(2, chunk.Len(), "Length should be 2")
		assert.False(chunk.IsEmpty(), "Chunk should not be empty")
		assert.False(chunk.IsFull(), "Chunk should not be full")

		// Full chunk
		chunk.Push(3)
		assert.Equal(3, chunk.Len(), "Length should be 3")
		assert.False(chunk.IsEmpty(), "Chunk should not be empty")
		assert.True(chunk.IsFull(), "Chunk should be full")

		// After popping all items
		chunk.Pop()
		chunk.Pop()
		chunk.Pop()
		assert.Equal(0, chunk.Len(), "Length should be 0")
		assert.True(chunk.IsEmpty(), "Chunk should be empty")
		assert.True(chunk.IsFull(), "Chunk should still be full (write index at capacity)")
	})

	t.Run("Length edge case - single capacity", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](1)

		assert.Equal(0, chunk.Len(), "Single capacity chunk should start with length 0")

		chunk.Push(42)
		assert.Equal(1, chunk.Len(), "Single capacity chunk should have length 1 after push")

		chunk.Pop()
		assert.Equal(0, chunk.Len(), "Single capacity chunk should have length 0 after pop")
	})
}

func TestChunk_IsFull(t *testing.T) {
	t.Run("Empty chunk is not full", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](5)

		assert.False(chunk.IsFull(), "Empty chunk should not be full")
	})

	t.Run("Partially filled chunk is not full", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](5)
		chunk.Push(1)
		chunk.Push(2)

		assert.False(chunk.IsFull(), "Partially filled chunk should not be full")
	})

	t.Run("Completely filled chunk is full", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](3)
		chunk.Push(1)
		chunk.Push(2)
		chunk.Push(3)

		assert.True(chunk.IsFull(), "Completely filled chunk should be full")
	})

	t.Run("Zero capacity chunk is always full", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](0)

		assert.True(chunk.IsFull(), "Zero capacity chunk should always be full")
	})

	t.Run("Chunk remains full after failed push", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](2)
		chunk.Push(1)
		chunk.Push(2)
		chunk.Push(3) // This should fail

		assert.True(chunk.IsFull(), "Chunk should remain full after failed push")
	})
}

func TestChunk_IsEmpty(t *testing.T) {
	t.Run("New chunk is empty", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](5)

		assert.True(chunk.IsEmpty(), "New chunk should be empty")
	})

	t.Run("Chunk with items is not empty", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](5)
		chunk.Push(1)

		assert.False(chunk.IsEmpty(), "Chunk with items should not be empty")
	})

	t.Run("Chunk becomes empty after popping all items", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](3)
		chunk.Push(1)
		chunk.Push(2)

		chunk.Pop()
		assert.False(chunk.IsEmpty(), "Chunk should not be empty with remaining items")

		chunk.Pop()
		assert.True(chunk.IsEmpty(), "Chunk should be empty after popping all items")
	})

	t.Run("Zero capacity chunk is always empty", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](0)

		assert.True(chunk.IsEmpty(), "Zero capacity chunk should always be empty")
	})
}

func TestChunk_Push(t *testing.T) {
	t.Run("Push to empty chunk succeeds", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](5)

		success := chunk.Push(42)
		assert.True(success, "Push to empty chunk should succeed")
		assert.Equal(1, chunk.NextWriteIndex, "NextWriteIndex should be incremented")
		assert.Equal(42, chunk.Data[0], "Item should be stored at correct position")
	})

	t.Run("Push multiple items succeeds", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[string](3)

		assert.True(chunk.Push("first"), "First push should succeed")
		assert.True(chunk.Push("second"), "Second push should succeed")
		assert.True(chunk.Push("third"), "Third push should succeed")

		assert.Equal(3, chunk.NextWriteIndex, "NextWriteIndex should be 3")
		assert.Equal("first", chunk.Data[0], "First item should be at index 0")
		assert.Equal("second", chunk.Data[1], "Second item should be at index 1")
		assert.Equal("third", chunk.Data[2], "Third item should be at index 2")
	})

	t.Run("Push to full chunk fails", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](2)
		chunk.Push(1)
		chunk.Push(2)

		success := chunk.Push(3)
		assert.False(success, "Push to full chunk should fail")
		assert.Equal(2, chunk.NextWriteIndex, "NextWriteIndex should remain unchanged")
	})

	t.Run("Push to zero capacity chunk fails", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](0)

		success := chunk.Push(1)
		assert.False(success, "Push to zero capacity chunk should fail")
		assert.Equal(0, chunk.NextWriteIndex, "NextWriteIndex should remain 0")
	})

	t.Run("Push different types", func(t *testing.T) {
		t.Run("Strings", func(t *testing.T) {
			assert := assert.New(t)
			chunk := NewChunk[string](2)

			assert.True(chunk.Push("hello"), "String push should succeed")
			assert.True(chunk.Push("world"), "Second string push should succeed")
			assert.Equal("hello", chunk.Data[0], "First string should be stored correctly")
			assert.Equal("world", chunk.Data[1], "Second string should be stored correctly")
		})

		t.Run("Pointers", func(t *testing.T) {
			assert := assert.New(t)
			chunk := NewChunk[*int](2)
			val1, val2 := 42, 84

			assert.True(chunk.Push(&val1), "Pointer push should succeed")
			assert.True(chunk.Push(&val2), "Second pointer push should succeed")
			assert.Equal(&val1, chunk.Data[0], "First pointer should be stored correctly")
			assert.Equal(&val2, chunk.Data[1], "Second pointer should be stored correctly")
		})
	})
}

func TestChunk_Pop(t *testing.T) {
	t.Run("Pop from empty chunk fails", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](5)

		item, ok := chunk.Pop()
		assert.False(ok, "Pop from empty chunk should fail")
		assert.Equal(0, item, "Item should be zero value")
		assert.Equal(0, chunk.NextReadIndex, "NextReadIndex should remain 0")
	})

	t.Run("Pop from chunk with items succeeds", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](5)
		chunk.Push(42)
		chunk.Push(84)

		item, ok := chunk.Pop()
		assert.True(ok, "Pop should succeed")
		assert.Equal(42, item, "Should return first item")
		assert.Equal(1, chunk.NextReadIndex, "NextReadIndex should be incremented")
		assert.Equal(0, chunk.Data[0], "Popped position should be cleared")
	})

	t.Run("Pop multiple items in FIFO order", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[string](3)
		chunk.Push("first")
		chunk.Push("second")
		chunk.Push("third")

		item1, ok1 := chunk.Pop()
		assert.True(ok1, "First pop should succeed")
		assert.Equal("first", item1, "Should return first item")

		item2, ok2 := chunk.Pop()
		assert.True(ok2, "Second pop should succeed")
		assert.Equal("second", item2, "Should return second item")

		item3, ok3 := chunk.Pop()
		assert.True(ok3, "Third pop should succeed")
		assert.Equal("third", item3, "Should return third item")

		assert.Equal(3, chunk.NextReadIndex, "NextReadIndex should be 3")
	})

	t.Run("Pop from zero capacity chunk fails", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](0)

		item, ok := chunk.Pop()
		assert.False(ok, "Pop from zero capacity chunk should fail")
		assert.Equal(0, item, "Item should be zero value")
	})

	t.Run("Pop clears memory references", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[*int](2)
		val1, val2 := 42, 84
		chunk.Push(&val1)
		chunk.Push(&val2)

		item, ok := chunk.Pop()
		assert.True(ok, "Pop should succeed")
		assert.Equal(&val1, item, "Should return correct pointer")
		assert.Nil(chunk.Data[0], "Popped position should be cleared to prevent memory leak")
	})

	t.Run("Pop after chunk becomes empty", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](2)
		chunk.Push(1)
		chunk.Push(2)

		// Pop all items
		chunk.Pop()
		chunk.Pop()

		// Try to pop from empty chunk
		item, ok := chunk.Pop()
		assert.False(ok, "Pop from empty chunk should fail")
		assert.Equal(0, item, "Item should be zero value")
	})
}

func TestChunk_MixedOperations(t *testing.T) {
	t.Run("Push and pop interleaved", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](3)

		// Push some items
		assert.True(chunk.Push(1), "First push should succeed")
		assert.True(chunk.Push(2), "Second push should succeed")

		// Pop one item
		item, ok := chunk.Pop()
		assert.True(ok, "Pop should succeed")
		assert.Equal(1, item, "Should return first item")

		// Push more items
		assert.True(chunk.Push(3), "Third push should succeed")
		assert.False(chunk.Push(4), "Fourth push should fail - chunk is full (write index at capacity)")

		// Now chunk should be full (NextWriteIndex is at capacity)
		assert.True(chunk.IsFull(), "Chunk should be full")
		assert.False(chunk.Push(5), "Push to full chunk should fail")

		// Pop remaining items
		item, ok = chunk.Pop()
		assert.True(ok && item == 2, "Should pop second item")
		item, ok = chunk.Pop()
		assert.True(ok && item == 3, "Should pop third item")

		assert.True(chunk.IsEmpty(), "Chunk should be empty")
	})

	t.Run("Fill, empty, and refill chunk", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](2)

		// Fill chunk
		assert.True(chunk.Push(1), "First push should succeed")
		assert.True(chunk.Push(2), "Second push should succeed")
		assert.True(chunk.IsFull(), "Chunk should be full")

		// Empty chunk
		chunk.Pop()
		chunk.Pop()
		assert.True(chunk.IsEmpty(), "Chunk should be empty")

		// Refill chunk (note: this won't work with current implementation due to indices)
		// This test shows the limitation of the current design
		assert.False(chunk.Push(3), "Push should fail because NextWriteIndex is at capacity")
	})
}

func TestChunk_EdgeCases(t *testing.T) {
	t.Run("Single capacity chunk", func(t *testing.T) {
		assert := assert.New(t)
		chunk := NewChunk[int](1)

		assert.True(chunk.IsEmpty(), "Single capacity chunk should start empty")
		assert.False(chunk.IsFull(), "Single capacity chunk should not start full")

		assert.True(chunk.Push(42), "Push to single capacity chunk should succeed")
		assert.True(chunk.IsFull(), "Single capacity chunk should be full after push")
		assert.False(chunk.IsEmpty(), "Single capacity chunk should not be empty after push")

		item, ok := chunk.Pop()
		assert.True(ok, "Pop should succeed")
		assert.Equal(42, item, "Should return correct item")
		assert.True(chunk.IsEmpty(), "Single capacity chunk should be empty after pop")
		assert.True(chunk.IsFull(), "Single capacity chunk should still be full after pop (write index at capacity)")
	})

	t.Run("Chunk with struct types", func(t *testing.T) {
		type TestStruct struct {
			ID   int
			Name string
		}

		assert := assert.New(t)
		chunk := NewChunk[TestStruct](2)

		item1 := TestStruct{ID: 1, Name: "first"}
		item2 := TestStruct{ID: 2, Name: "second"}

		assert.True(chunk.Push(item1), "Push struct should succeed")
		assert.True(chunk.Push(item2), "Push second struct should succeed")

		popped1, ok1 := chunk.Pop()
		assert.True(ok1, "Pop should succeed")
		assert.Equal(item1, popped1, "Should return first struct")

		popped2, ok2 := chunk.Pop()
		assert.True(ok2, "Pop should succeed")
		assert.Equal(item2, popped2, "Should return second struct")
	})
}

func TestChunk_Next(t *testing.T) {
	t.Run("Next pointer functionality", func(t *testing.T) {
		assert := assert.New(t)
		chunk1 := NewChunk[int](2)
		chunk2 := NewChunk[int](2)

		assert.Nil(chunk1.Next, "Next should initially be nil")

		chunk1.Next = chunk2
		assert.Equal(chunk2, chunk1.Next, "Next should point to second chunk")
		assert.Nil(chunk2.Next, "Second chunk's Next should be nil")
	})
}
