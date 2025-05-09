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
}
