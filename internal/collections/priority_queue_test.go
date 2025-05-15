package collections

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

		assert.Equal(0, pq.Len(), "queue should be empty after close")

		// Verify the queue is still usable after close
		pq.Enqueue(3, 1)
		assert.Equal(1, pq.Len(), "queue should have length 1 after adding to closed queue")
	})
}
