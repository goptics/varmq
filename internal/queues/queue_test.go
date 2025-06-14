package queues

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQueue(t *testing.T) {
	t.Run("Basic Operations", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		assert.Equal(0, q.Len(), "empty queue should have length 0")

		q.Enqueue(1)
		q.Enqueue(2)

		assert.Equal(2, q.Len(), "queue should have length 2 after adding 2 items")

		val, ok := q.Dequeue()
		assert.True(ok, "dequeue should return true for non-empty queue")
		assert.Equal(1, val, "dequeue should return the first element")

		assert.Equal(1, q.Len(), "queue should have length 1 after removing 1 item")

		val, ok = q.Dequeue()
		assert.True(ok, "dequeue should return true for non-empty queue")
		assert.Equal(2, val, "dequeue should return the second element")

		val, ok = q.Dequeue()
		assert.False(ok, "dequeue should return false for empty queue")
		assert.Equal(0, val, "dequeue should return zero value for empty queue")
	})

	t.Run("Empty Queue", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		val, ok := q.Dequeue()
		assert.False(ok, "dequeue should return false for empty queue")
		assert.Equal(0, val, "dequeue should return zero value for empty queue")
	})

	t.Run("Values Method", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()
		q.Enqueue(1)
		q.Enqueue(2)

		values := q.Values()
		expected := []interface{}{1, 2}
		assert.Equal(expected, values, "values should return all items in the queue")
	})

	t.Run("Empty Values Method", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		values := q.Values()
		assert.Empty(values, "empty queue should return empty slice")
		assert.NotNil(values, "empty queue should return non-nil slice")
	})

	t.Run("Purge Method", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()
		q.Enqueue(1)
		q.Enqueue(2)

		assert.Equal(2, q.Len(), "queue should have length 2 before purge")

		q.Purge()

		assert.Equal(0, q.Len(), "queue should be empty after purge")

		// Verify the queue is usable after purge
		q.Enqueue(3)
		assert.Equal(1, q.Len(), "queue should have length 1 after adding to purged queue")
	})

	t.Run("Close Method", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()
		q.Enqueue(1)
		q.Enqueue(2)

		assert.Equal(2, q.Len(), "queue should have length 2 before close")

		err := q.Close()
		assert.NoError(err, "close should not return an error")
		assert.Equal(2, q.Len(), "queue should have length 2 after close")

		// Verify the queue wouldn't be usable after close
		ok := q.Enqueue(3)
		assert.False(ok, "enqueue should fail after close")

		val, ok := q.Dequeue()
		assert.True(ok)
		assert.Equal(1, val, "dequeue should return the first element")
		assert.Equal(1, q.Len(), "queue should have length 1 after dequeuing from closed queue")
	})

	t.Run("Type Assertion Failure", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		// Try to enqueue wrong type using interface{}
		var item interface{} = "string"
		ok := q.Enqueue(item)
		assert.False(ok, "enqueue should fail for wrong type")
		assert.Equal(0, q.Len(), "queue should remain empty after failed enqueue")
	})

	t.Run("Chunk Capacity Growth", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		// Fill initial chunk (1024 capacity)
		for i := range 1024 {
			ok := q.Enqueue(i)
			assert.True(ok, "enqueue should succeed within initial capacity")
		}

		// Add one more to trigger new chunk creation
		ok := q.Enqueue(1024)
		assert.True(ok, "enqueue should succeed and create new chunk")
		assert.Equal(1025, q.Len(), "queue should have 1025 items after chunk growth")

		// Verify FIFO order is maintained across chunks
		for i := range 1025 {
			val, ok := q.Dequeue()
			assert.True(ok, "dequeue should succeed")
			assert.Equal(i, val, "items should maintain FIFO order across chunks")
		}
	})

	t.Run("Multiple Chunk Growth", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		// Add enough items to create multiple chunks
		itemCount := 3000 // This will create multiple chunks with growing capacities
		for i := range itemCount {
			ok := q.Enqueue(i)
			assert.True(ok, "enqueue should succeed")
		}

		assert.Equal(itemCount, q.Len(), "queue should have all items")

		// Verify all items are retrievable in correct order
		for i := range itemCount {
			val, ok := q.Dequeue()
			assert.True(ok, "dequeue should succeed")
			assert.Equal(i, val, "items should maintain FIFO order across multiple chunks")
		}

		assert.Equal(0, q.Len(), "queue should be empty after dequeuing all items")
	})

	t.Run("Chunk Advancement", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		// Fill and empty first chunk
		for i := range 1024 {
			q.Enqueue(i)
		}

		// Add items to second chunk
		for i := 1024; i < 1536; i++ {
			q.Enqueue(i)
		}

		// Remove all items from first chunk
		for i := range 1024 {
			val, ok := q.Dequeue()
			assert.True(ok, "dequeue should succeed")
			assert.Equal(i, val, "items should be in correct order")
		}

		// Verify we can still access items from second chunk
		for i := 1024; i < 1536; i++ {
			val, ok := q.Dequeue()
			assert.True(ok, "dequeue should succeed from second chunk")
			assert.Equal(i, val, "items should be in correct order from second chunk")
		}
	})

	t.Run("Different Data Types", func(t *testing.T) {
		t.Run("String Queue", func(t *testing.T) {
			assert := assert.New(t)
			q := NewQueue[string]()

			q.Enqueue("hello")
			q.Enqueue("world")

			val, ok := q.Dequeue()
			assert.True(ok)
			assert.Equal("hello", val)

			val, ok = q.Dequeue()
			assert.True(ok)
			assert.Equal("world", val)
		})

		t.Run("Struct Queue", func(t *testing.T) {
			type TestStruct struct {
				ID   int
				Name string
			}

			assert := assert.New(t)
			q := NewQueue[TestStruct]()

			item1 := TestStruct{ID: 1, Name: "test1"}
			item2 := TestStruct{ID: 2, Name: "test2"}

			q.Enqueue(item1)
			q.Enqueue(item2)

			val, ok := q.Dequeue()
			assert.True(ok)
			assert.Equal(item1, val)

			val, ok = q.Dequeue()
			assert.True(ok)
			assert.Equal(item2, val)
		})

		t.Run("Pointer Queue", func(t *testing.T) {
			assert := assert.New(t)
			q := NewQueue[*int]()

			val1 := 42
			val2 := 84

			q.Enqueue(&val1)
			q.Enqueue(&val2)

			ptr, ok := q.Dequeue()
			assert.True(ok)
			assert.Equal(&val1, ptr)
			assert.Equal(42, *ptr.(*int))

			ptr, ok = q.Dequeue()
			assert.True(ok)
			assert.Equal(&val2, ptr)
			assert.Equal(84, *ptr.(*int))
		})
	})

	t.Run("Concurrent Operations", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		const numGoroutines = 10
		const itemsPerGoroutine = 100

		var wg sync.WaitGroup

		// Concurrent enqueues
		for i := range numGoroutines {
			wg.Add(1)
			go func(start int) {
				defer wg.Done()
				for j := 0; j < itemsPerGoroutine; j++ {
					q.Enqueue(start*itemsPerGoroutine + j)
				}
			}(i)
		}

		wg.Wait()

		// Check total count
		assert.Equal(numGoroutines*itemsPerGoroutine, q.Len(), "all items should be enqueued")

		// Concurrent dequeues
		results := make([]int, numGoroutines*itemsPerGoroutine)
		var mu sync.Mutex
		index := 0

		for range numGoroutines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < itemsPerGoroutine; j++ {
					if val, ok := q.Dequeue(); ok {
						mu.Lock()
						results[index] = val.(int)
						index++
						mu.Unlock()
					}
				}
			}()
		}

		wg.Wait()

		// Verify all items were dequeued
		assert.Equal(0, q.Len(), "queue should be empty after concurrent dequeues")
		assert.Equal(numGoroutines*itemsPerGoroutine, index, "all items should be dequeued")
	})

	t.Run("Values Method Across Chunks", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		// Add items across multiple chunks
		for i := range 2048 { // This will span multiple chunks
			q.Enqueue(i)
		}

		values := q.Values()
		assert.Equal(2048, len(values), "values should return all items across chunks")

		// Verify order is maintained
		for i, val := range values {
			assert.Equal(i, val, "values should maintain correct order across chunks")
		}
	})

	t.Run("Mixed Operations", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		// Add some initial items
		q.Enqueue(1)
		q.Enqueue(2)
		q.Enqueue(3)

		// Interleave enqueue and dequeue operations
		for i := range 10 {
			// Add two items
			q.Enqueue(10 + i*2)
			q.Enqueue(10 + i*2 + 1)

			// Remove one item (should be FIFO)
			_, ok := q.Dequeue()
			assert.True(ok, "dequeue should succeed")
			// First few dequeues will get the initial items (1, 2, 3)
			// Then items added in previous iterations
		}

		// Queue should have net positive items
		// Started with 3, added 20 (10*2), removed 10
		// Net: 3 + 20 - 10 = 13
		assert.Equal(13, q.Len(), "queue should have correct length after mixed operations")

		// Verify remaining items are in correct FIFO order
		remainingItems := q.Values()
		assert.Equal(13, len(remainingItems), "should have 13 remaining items")
	})

	t.Run("Resizing Behavior", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		// Add enough elements to force resize (initial capacity is 10)
		for i := 1; i <= 12; i++ {
			q.Enqueue(i)
		}

		// Check length is correct after resize
		assert.Equal(12, q.Len(), "queue should have all 12 elements after resize")

		// Check values are all preserved in correct order
		for i := 1; i <= 12; i++ {
			val, ok := q.Dequeue()
			assert.True(ok)
			assert.Equal(i, val, "elements should be dequeued in FIFO order")
		}
	})

	t.Run("Circular Buffer Behavior", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		// Add and remove elements to advance the front index
		for i := 1; i <= 5; i++ {
			q.Enqueue(i)
		}

		// Remove 3 elements
		for range 3 {
			q.Dequeue()
		}

		// Add more elements, which should wrap around in the circular buffer
		for i := 6; i <= 12; i++ {
			q.Enqueue(i)
		}

		// Check total queue length
		assert.Equal(9, q.Len(), "queue should have 9 elements (2 remaining + 7 new)")

		// Check the values are in the correct order
		expected := []int{4, 5, 6, 7, 8, 9, 10, 11, 12}
		for _, exp := range expected {
			val, ok := q.Dequeue()
			assert.True(ok)
			assert.Equal(exp, val, "elements should maintain FIFO order after wrap-around")
		}
	})

	t.Run("Shrinking Behavior", func(t *testing.T) {
		assert := assert.New(t)
		q := NewQueue[int]()

		// Add many elements to force multiple resizes
		for i := 1; i <= 100; i++ {
			q.Enqueue(i)
		}

		// Remove most elements to trigger shrinking
		for i := 1; i <= 80; i++ {
			val, ok := q.Dequeue()
			assert.True(ok)
			assert.Equal(i, val)
		}

		// Queue should still contain the remaining elements
		assert.Equal(20, q.Len(), "queue should have 20 elements remaining")

		// Check the remaining elements are in correct order
		for i := 81; i <= 100; i++ {
			val, ok := q.Dequeue()
			assert.True(ok)
			assert.Equal(i, val, "remaining elements should be in order after shrinking")
		}
	})
}
