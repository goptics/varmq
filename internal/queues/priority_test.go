package queues

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPriorityQueue(t *testing.T) {
	t.Run("Enqueue Type Assertion Failure", func(t *testing.T) {
		assert := assert.New(t)

		// Create a queue for integers
		pq := NewPriorityQueue[int]()

		// Try to enqueue a string (which should fail)
		result := pq.Enqueue("not an integer", 1)
		assert.False(result, "enqueue should return false when type assertion fails")

		// Make sure queue is still empty
		assert.Equal(0, pq.Len(), "queue should remain empty after failed enqueue")

		// Verify that valid items can still be added after a failed attempt
		result = pq.Enqueue(42, 1)
		assert.True(result, "enqueue should succeed with correct type")
		assert.Equal(1, pq.Len(), "queue should contain one item after successful enqueue")
	})

	t.Run("Basic Operations", func(t *testing.T) {
		assert := assert.New(t)
		pq := NewPriorityQueue[int]()

		assert.Equal(0, pq.Len(), "empty queue should have length 0")

		pq.Enqueue(1, 2)
		pq.Enqueue(2, 1)
		pq.Enqueue(3, 1)

		assert.Equal(3, pq.Len(), "queue should have length 3 after adding 3 items")

		val, ok := pq.Dequeue()
		assert.True(ok, "dequeue should return true for non-empty queue")
		assert.Equal(2, val, "first dequeue should return the highest priority element (lowest value priority)")

		assert.Equal(2, pq.Len(), "queue should have length 2 after removing 1 item")

		val, ok = pq.Dequeue()
		assert.True(ok, "dequeue should return true for non-empty queue")
		assert.Equal(3, val, "second dequeue should return element with same priority but higher insertion index")

		val, ok = pq.Dequeue()
		assert.True(ok, "dequeue should return true for non-empty queue")
		assert.Equal(1, val, "third dequeue should return the lowest priority element")

		val, ok = pq.Dequeue()
		assert.False(ok, "dequeue should return false for empty queue")
		assert.Equal(0, val, "dequeue should return zero value for empty queue")
	})

	t.Run("Empty Queue", func(t *testing.T) {
		assert := assert.New(t)
		pq := NewPriorityQueue[int]()

		val, ok := pq.Dequeue()
		assert.False(ok, "dequeue should return false for empty queue")
		assert.Equal(0, val, "dequeue should return zero value for empty queue")
	})

	t.Run("Values Method", func(t *testing.T) {
		assert := assert.New(t)
		pq := NewPriorityQueue[int]()
		pq.Enqueue(1, 2)
		pq.Enqueue(2, 1)

		values := pq.Values()
		expected := []interface{}{2, 1}
		assert.Equal(expected, values, "values should return all items in the queue")
	})

	t.Run("Purge Method", func(t *testing.T) {
		assert := assert.New(t)
		pq := NewPriorityQueue[int]()
		pq.Enqueue(1, 2)
		pq.Enqueue(2, 1)

		assert.Equal(2, pq.Len(), "queue should have length 2 before purge")

		pq.Purge()

		assert.Equal(0, pq.Len(), "queue should be empty after purge")

		// Verify the queue is usable after purge
		pq.Enqueue(3, 1)
		assert.Equal(1, pq.Len(), "queue should have length 1 after adding to purged queue")
	})

	t.Run("Close Method", func(t *testing.T) {
		assert := assert.New(t)
		pq := NewPriorityQueue[int]()
		pq.Enqueue(1, 2)
		pq.Enqueue(2, 1)

		assert.Equal(2, pq.Len(), "queue should have length 2 before close")

		err := pq.Close()
		assert.NoError(err, "close should not return an error")
		assert.Equal(2, pq.Len(), "queue should have length 2 after close")

		// Verify the queue wouldn't be usable after close
		ok := pq.Enqueue(3, 1)
		assert.False(ok, "enqueue should fail after close")

		val, ok := pq.Dequeue()
		assert.True(ok)
		assert.Equal(2, val, "dequeue should return the first element")
		assert.Equal(1, pq.Len(), "queue should have length 1 after dequeuing from closed queue")
	})

	t.Run("SetCapacity", func(t *testing.T) {
		t.Run("sets capacity correctly", func(t *testing.T) {
			pq := NewPriorityQueue[int]()
			pq.SetCapacity(5)
			assert.Equal(t, 5, pq.capacity)
		})

		t.Run("clamps negative values to zero", func(t *testing.T) {
			pq := NewPriorityQueue[int]()
			pq.SetCapacity(-10)
			assert.Equal(t, 0, pq.capacity)
		})

		t.Run("zero means unlimited", func(t *testing.T) {
			pq := NewPriorityQueue[int]()
			pq.SetCapacity(0)

			for i := range 100 {
				ok := pq.Enqueue(i, i)
				assert.True(t, ok, "enqueue should always succeed with zero capacity")
			}
			assert.Equal(t, 100, pq.Len())
		})

		t.Run("enqueue blocked at capacity", func(t *testing.T) {
			pq := NewPriorityQueue[int]()
			pq.SetCapacity(3)

			assert.True(t, pq.Enqueue(1, 1))
			assert.True(t, pq.Enqueue(2, 2))
			assert.True(t, pq.Enqueue(3, 3))
			assert.False(t, pq.Enqueue(4, 4), "enqueue should fail when at capacity")
			assert.Equal(t, 3, pq.Len())
		})

		t.Run("enqueue resumes after dequeue frees space", func(t *testing.T) {
			pq := NewPriorityQueue[int]()
			pq.SetCapacity(2)

			pq.Enqueue(1, 1)
			pq.Enqueue(2, 2)
			assert.False(t, pq.Enqueue(3, 3), "should be at capacity")

			pq.Dequeue()
			assert.True(t, pq.Enqueue(3, 3), "should succeed after dequeue frees space")
			assert.Equal(t, 2, pq.Len())
		})
	})
}
