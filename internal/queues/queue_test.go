package queues

import (
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

		assert.Equal(0, q.Len(), "queue should be empty after close")

		// Verify the queue is still usable after close
		q.Enqueue(3)
		assert.Equal(1, q.Len(), "queue should have length 1 after adding to closed queue")
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
		for i := 0; i < 3; i++ {
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

	// TestQueueResize explicitly tests the resize method
	t.Run("ExplicitResizeMethod", func(t *testing.T) {
		t.Run("Empty Queue", func(t *testing.T) {
			assert := assert.New(t)
			q := NewQueue[int]()

			// Explicitly call resize
			q.resize(50)

			// Verify the queue after resize
			assert.Equal(50, len(q.elements), "Capacity should be 50 after resize")
			assert.Equal(0, q.size, "Size should remain 0")
			assert.Equal(0, q.front, "Front should be reset to 0")
		})

		t.Run("Queue With Elements", func(t *testing.T) {
			assert := assert.New(t)
			q := NewQueue[int]()

			// Add some elements
			for i := 1; i <= 5; i++ {
				q.Enqueue(i)
			}

			// Explicitly call resize
			q.resize(200)

			// Verify the queue after resize
			assert.Equal(200, len(q.elements), "Capacity should be 200 after resize")
			assert.Equal(5, q.size, "Size should remain 5")
			assert.Equal(0, q.front, "Front should be reset to 0")

			// Check that elements are preserved in order
			for i := 1; i <= 5; i++ {
				val, ok := q.Dequeue()
				assert.True(ok)
				assert.Equal(i, val, "Elements should be preserved in order after resize")
			}
		})

		t.Run("Circular Buffer Wrap-Around", func(t *testing.T) {
			assert := assert.New(t)
			q := NewQueue[int]()

			// Add some elements
			for i := 1; i <= 5; i++ {
				q.Enqueue(i)
			}

			// Remove a few to advance the front index
			for i := 1; i <= 3; i++ {
				val, _ := q.Dequeue()
				assert.Equal(i, val)
			}

			// Add more elements to cause wrap-around
			for i := 6; i <= 8; i++ {
				q.Enqueue(i)
			}

			// At this point, elements should be [4,5,6,7,8] with front at some index > 0
			frontBeforeResize := q.front
			assert.Equal(5, q.size)
			assert.Greater(frontBeforeResize, 0, "Front should be advanced")

			// Now explicitly call resize
			q.resize(10)

			// Verify the queue after resize
			assert.Equal(10, len(q.elements), "Capacity should be 10 after resize")
			assert.Equal(5, q.size, "Size should remain 5")
			assert.Equal(0, q.front, "Front should be reset to 0")

			// The elements should now be in contiguous order at the beginning of the slice
			expected := []int{4, 5, 6, 7, 8}
			for i, exp := range expected {
				assert.Equal(exp, q.elements[i], "Element at index %d should be %d", i, exp)
			}

			// Verify FIFO behavior is maintained
			for _, exp := range expected {
				val, ok := q.Dequeue()
				assert.True(ok)
				assert.Equal(exp, val, "Elements should be dequeued in correct order after resize")
			}
		})

		t.Run("Resize To Size Equal To Element Count", func(t *testing.T) {
			assert := assert.New(t)
			q := NewQueue[int]()

			// Add some elements
			for i := 1; i <= 5; i++ {
				q.Enqueue(i)
			}

			// Resize to exactly the size of the queue
			q.resize(5)

			// Verify the queue after resize
			assert.Equal(5, len(q.elements), "Capacity should equal size after resize")
			assert.Equal(5, q.size, "Size should remain 5")
			assert.Equal(0, q.front, "Front should be reset to 0")

			// Verify elements are still correct
			for i := 1; i <= 5; i++ {
				val, ok := q.Dequeue()
				assert.True(ok)
				assert.Equal(i, val, "Elements should be dequeued in correct order after tight resize")
			}
		})
	})
}
