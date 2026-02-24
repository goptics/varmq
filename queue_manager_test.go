package varmq

import (
	"math"
	"testing"

	"github.com/goptics/varmq/internal/queues"
	"github.com/stretchr/testify/assert"
)

func TestQueueManagerNext(t *testing.T) {
	t.Run("Strategy: RoundRobin", func(t *testing.T) {
		qm := newQueueManager(RoundRobin)

		// Create and register three queues
		q1 := queues.NewQueue[any]()
		q2 := queues.NewQueue[any]()
		q3 := queues.NewQueue[any]()

		// Add items to queues to distinguish them
		q1.Enqueue("q1-item")
		q2.Enqueue("q2-item1")
		q2.Enqueue("q2-item2") // q2 has more items
		q3.Enqueue("q3-item")

		qm.Register(q1, math.MaxInt)
		qm.Register(q2, math.MaxInt)
		qm.Register(q3, math.MaxInt)

		// Test round-robin behavior
		queue1, err := qm.next()
		assert.NoError(t, err)

		queue2, err := qm.next()
		assert.NoError(t, err)

		queue3, err := qm.next()
		assert.NoError(t, err)

		// Fourth call should cycle back to first queue
		queue4, err := qm.next()
		assert.NoError(t, err)

		// Order should be maintained in round-robin
		assert.Equal(t, queue1, queue4, "Round-robin should cycle back to first queue")
		assert.NotEqual(t, queue1, queue2, "Different queues should be returned in sequence")
		assert.NotEqual(t, queue2, queue3, "Different queues should be returned in sequence")
	})

	t.Run("Strategy: MaxLen", func(t *testing.T) {
		qm := newQueueManager(MaxLen)

		// Create and register three queues with different lengths
		q1 := queues.NewQueue[any]()
		q2 := queues.NewQueue[any]()
		q3 := queues.NewQueue[any]()

		// Add different number of items to each queue
		q1.Enqueue("q1-item")  // 1 item
		q2.Enqueue("q2-item1") // 2 items
		q2.Enqueue("q2-item2")
		q3.Enqueue("q3-item1") // 3 items
		q3.Enqueue("q3-item2")
		q3.Enqueue("q3-item3")

		qm.Register(q1, math.MaxInt)
		qm.Register(q2, math.MaxInt)
		qm.Register(q3, math.MaxInt)

		// MaxLen should always return the queue with the most items
		queue, err := qm.next()
		assert.NoError(t, err)
		assert.Equal(t, 3, queue.Len(), "MaxLen strategy should return queue with the most items")
		assert.Equal(t, q3, queue, "MaxLen strategy should return q3 with 3 items")
	})

	t.Run("Strategy: MinLen", func(t *testing.T) {
		qm := newQueueManager(MinLen)

		// Create and register three queues with different lengths
		q1 := queues.NewQueue[any]()
		q2 := queues.NewQueue[any]()
		q3 := queues.NewQueue[any]()

		// Add different number of items to each queue
		q1.Enqueue("q1-item")  // 1 item
		q2.Enqueue("q2-item1") // 2 items
		q2.Enqueue("q2-item2")
		q3.Enqueue("q3-item1") // 3 items
		q3.Enqueue("q3-item2")
		q3.Enqueue("q3-item3")

		qm.Register(q1, math.MaxInt)
		qm.Register(q2, math.MaxInt)
		qm.Register(q3, math.MaxInt)

		// MinLen should always return the queue with the fewest items
		queue, err := qm.next()
		assert.NoError(t, err)
		assert.Equal(t, 1, queue.Len(), "MinLen strategy should return queue with the fewest items")
		assert.Equal(t, q1, queue, "MinLen strategy should return q1 with 1 item")
	})

	t.Run("Strategy: Priority", func(t *testing.T) {
		qm := newQueueManager(Priority)

		// Create and register three queues
		q1 := queues.NewQueue[any]()
		q2 := queues.NewQueue[any]()
		q3 := queues.NewQueue[any]()

		// Add different number of items to each queue
		q1.Enqueue("q1-item")  // 1 item
		q2.Enqueue("q2-item1") // 2 items
		q2.Enqueue("q2-item2")
		q3.Enqueue("q3-item1") // 1 item (will have highest priority)

		// Lower number = higher priority
		qm.Register(q1, 10)
		qm.Register(q2, 5)
		qm.Register(q3, 1)

		// Priority should always return the queue with the highest priority first, provided it has items
		queue, err := qm.next()
		assert.NoError(t, err)
		assert.Equal(t, q3, queue, "Priority strategy should return q3 with highest priority 1")

		// Remove q3 to test falling back to next highest priority
		qm.UnregisterItem(q3)

		queue, err = qm.next()
		assert.NoError(t, err)
		assert.Equal(t, q2, queue, "Priority strategy should return q2 with priority 5 after q3 is unregistered")
	})

	t.Run("No Queues Registered", func(t *testing.T) {
		// Test all strategies with no queues registered
		strategies := []Strategy{RoundRobin, MaxLen, MinLen, Priority}

		for _, strategy := range strategies {
			qm := newQueueManager(strategy)
			queue, err := qm.next()
			assert.Error(t, err, "Should return error when no queues are registered")
			assert.Nil(t, queue, "Should return nil queue when no queues are registered")
			assert.Equal(t, "no items registered", err.Error(), "Error message should indicate no items registered")
		}
	})

	t.Run("Invalid Strategy", func(t *testing.T) {
		// Create queue manager with an invalid strategy (out of range)
		invalidStrategy := Strategy(99)
		qm := newQueueManager(invalidStrategy)

		q := queues.NewQueue[any]()
		qm.Register(q, math.MaxInt)

		queue, err := qm.next()
		assert.Error(t, err, "Should return error for invalid strategy")
		assert.Nil(t, queue, "Should return nil queue for invalid strategy")
		assert.Equal(t, "invalid strategy type", err.Error(), "Error message should indicate invalid strategy")
	})
}
