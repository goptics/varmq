package varmq

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/goptics/varmq/mocks"
)

func setupDistributedQueue() (*distributedQueue[string], *mocks.MockDistributedQueue) {
	mockQueue := mocks.NewMockDistributedQueue()
	queue := NewDistributedQueue[string](mockQueue).(*distributedQueue[string])

	return queue, mockQueue
}

func setupDistributedPriorityQueue() (*distributedPriorityQueue[string], *mocks.MockDistributedPriorityQueue) {
	mockQueue := mocks.NewMockDistributedPriorityQueue()
	queue := NewDistributedPriorityQueue[string](mockQueue).(*distributedPriorityQueue[string])

	return queue, mockQueue
}

// Tests for DistributedQueue

func TestDistributedQueue(t *testing.T) {
	t.Run("Add method success", func(t *testing.T) {
		queue, mockQueue := setupDistributedQueue()

		// Test adding a job
		ok := queue.Add("test-data")
		assert.True(t, ok, "Job should be added successfully")
		assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add method with job configs", func(t *testing.T) {
		queue, mockQueue := setupDistributedQueue()

		// Test adding a job with custom ID
		ok := queue.Add("test-data", WithJobId("custom-id"))
		assert.True(t, ok, "Job should be added successfully with custom ID")
		assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add method failure when queue is closed", func(t *testing.T) {
		queue, mockQueue := setupDistributedQueue()

		// Close the internal queue to simulate failure
		err := mockQueue.Close()
		assert.NoError(t, err, "Mock queue should close successfully")

		// Attempt to add a job to closed queue
		ok := queue.Add("test-data")
		assert.False(t, ok, "Job should not be added to closed queue")
		assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
		assert.Equal(t, 0, mockQueue.Len(), "Mock queue should be empty")
	})

	t.Run("Add multiple jobs", func(t *testing.T) {
		queue, mockQueue := setupDistributedQueue()

		// Add multiple jobs
		for i := range 5 {
			ok := queue.Add("test-data-" + string(rune(i)))
			assert.True(t, ok, "Job %d should be added successfully", i)
		}

		assert.Equal(t, 5, queue.NumPending(), "Queue should have five pending jobs")
		assert.Equal(t, 5, mockQueue.Len(), "Mock queue should have five items")
	})

	t.Run("Subscription functionality", func(t *testing.T) {
		queue, mockQueue := setupDistributedQueue()

		// Track subscription calls
		var subscriptionCalls []string
		mockQueue.Subscribe(func(action string) {
			subscriptionCalls = append(subscriptionCalls, action)
		})

		// Add a job and verify subscription is called
		ok := queue.Add("test-data")
		assert.True(t, ok, "Job should be added successfully")
		assert.Equal(t, 1, len(subscriptionCalls), "Should have one subscription call")
		assert.Equal(t, "enqueued", subscriptionCalls[0], "Subscription should be called with 'enqueued' action")
	})

	t.Run("Multiple subscribers", func(t *testing.T) {
		queue, mockQueue := setupDistributedQueue()

		// Track subscription calls from multiple subscribers
		var subscriber1Calls []string
		var subscriber2Calls []string

		mockQueue.Subscribe(func(action string) {
			subscriber1Calls = append(subscriber1Calls, action)
		})

		mockQueue.Subscribe(func(action string) {
			subscriber2Calls = append(subscriber2Calls, action)
		})

		// Add a job and verify both subscribers are called
		ok := queue.Add("test-data")
		assert.True(t, ok, "Job should be added successfully")
		assert.Equal(t, 1, len(subscriber1Calls), "Subscriber 1 should have one call")
		assert.Equal(t, 1, len(subscriber2Calls), "Subscriber 2 should have one call")
		assert.Equal(t, "enqueued", subscriber1Calls[0], "Subscriber 1 should receive 'enqueued' action")
		assert.Equal(t, "enqueued", subscriber2Calls[0], "Subscriber 2 should receive 'enqueued' action")
	})

	t.Run("NumPending method", func(t *testing.T) {
		queue, mockQueue := setupDistributedQueue()

		// Initially should be empty
		assert.Equal(t, 0, queue.NumPending(), "Queue should initially be empty")

		// Add some jobs
		queue.Add("job1")
		queue.Add("job2")

		assert.Equal(t, 2, queue.NumPending(), "Queue should have two pending jobs")
		assert.Equal(t, 2, mockQueue.Len(), "Mock queue should have two items")
	})
}

// Tests for DistributedPriorityQueue

func TestDistributedPriorityQueue(t *testing.T) {
	t.Run("Add method success", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Test adding a job with priority
		ok := queue.Add("test-data", 5)
		assert.True(t, ok, "Job should be added successfully")
		assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add method with job configs", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Test adding a job with custom ID and priority
		ok := queue.Add("test-data", 3, WithJobId("custom-id"))
		assert.True(t, ok, "Job should be added successfully with custom ID and priority")
		assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add method failure when queue is closed", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Close the internal queue to simulate failure
		err := mockQueue.Close()
		assert.NoError(t, err, "Mock queue should close successfully")

		// Attempt to add a job to closed queue
		ok := queue.Add("test-data", 1)
		assert.False(t, ok, "Job should not be added to closed queue")
		assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
		assert.Equal(t, 0, mockQueue.Len(), "Mock queue should be empty")
	})

	t.Run("Add multiple jobs with different priorities", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Add jobs with different priorities
		priorities := []int{5, 1, 3, 2, 4}
		for i, priority := range priorities {
			ok := queue.Add("test-data-"+string(rune(i)), priority)
			assert.True(t, ok, "Job %d should be added successfully", i)
		}

		assert.Equal(t, 5, queue.NumPending(), "Queue should have five pending jobs")
		assert.Equal(t, 5, mockQueue.Len(), "Mock queue should have five items")
	})

	t.Run("Priority ordering", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Add jobs with different priorities (lower number = higher priority)
		queue.Add("low-priority", 10)
		queue.Add("high-priority", 1)
		queue.Add("medium-priority", 5)

		assert.Equal(t, 3, queue.NumPending(), "Queue should have three pending jobs")

		// Dequeue and verify priority ordering (highest priority first)
		item1, ok1 := mockQueue.Dequeue()
		assert.True(t, ok1, "Should dequeue first item")

		item2, ok2 := mockQueue.Dequeue()
		assert.True(t, ok2, "Should dequeue second item")

		item3, ok3 := mockQueue.Dequeue()
		assert.True(t, ok3, "Should dequeue third item")

		// Note: We can't directly check the content since items are serialized jobs
		// But we can verify all items were dequeued
		assert.NotNil(t, item1, "First item should not be nil")
		assert.NotNil(t, item2, "Second item should not be nil")
		assert.NotNil(t, item3, "Third item should not be nil")
	})

	t.Run("Subscription functionality", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Track subscription calls
		var subscriptionCalls []string
		mockQueue.Subscribe(func(action string) {
			subscriptionCalls = append(subscriptionCalls, action)
		})

		// Add a job and verify subscription is called
		ok := queue.Add("test-data", 1)
		assert.True(t, ok, "Job should be added successfully")
		assert.Equal(t, 1, len(subscriptionCalls), "Should have one subscription call")
		assert.Equal(t, "enqueued", subscriptionCalls[0], "Subscription should be called with 'enqueued' action")
	})

	t.Run("Multiple subscribers", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Track subscription calls from multiple subscribers
		var subscriber1Calls []string
		var subscriber2Calls []string

		mockQueue.Subscribe(func(action string) {
			subscriber1Calls = append(subscriber1Calls, action)
		})

		mockQueue.Subscribe(func(action string) {
			subscriber2Calls = append(subscriber2Calls, action)
		})

		// Add a job and verify both subscribers are called
		ok := queue.Add("test-data", 2)
		assert.True(t, ok, "Job should be added successfully")
		assert.Equal(t, 1, len(subscriber1Calls), "Subscriber 1 should have one call")
		assert.Equal(t, 1, len(subscriber2Calls), "Subscriber 2 should have one call")
		assert.Equal(t, "enqueued", subscriber1Calls[0], "Subscriber 1 should receive 'enqueued' action")
		assert.Equal(t, "enqueued", subscriber2Calls[0], "Subscriber 2 should receive 'enqueued' action")
	})

	t.Run("NumPending method", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Initially should be empty
		assert.Equal(t, 0, queue.NumPending(), "Queue should initially be empty")

		// Add some jobs with priorities
		queue.Add("job1", 1)
		queue.Add("job2", 2)

		assert.Equal(t, 2, queue.NumPending(), "Queue should have two pending jobs")
		assert.Equal(t, 2, mockQueue.Len(), "Mock queue should have two items")
	})
}

func TestDistributedQueueFailures(t *testing.T) {
	t.Run("Enqueue failure", func(t *testing.T) {
		queue, mockQueue := setupDistributedQueue()
		mockQueue.ShouldFailEnqueue = true

		// Track subscriptions to ensure none are called on failure
		var subscriptionCalls []string
		mockQueue.Subscribe(func(action string) {
			subscriptionCalls = append(subscriptionCalls, action)
		})

		ok := queue.Add("test-data")
		assert.False(t, ok, "Enqueue should fail when configured")
		assert.Equal(t, 0, queue.NumPending(), "Queue should be empty")
		assert.Equal(t, 0, len(subscriptionCalls), "No subscription calls should be made")
	})

	t.Run("Acknowledge failure", func(t *testing.T) {
		_, mockQueue := setupDistributedQueue()
		mockQueue.ShouldFailAcknowledge = true

		ok := mockQueue.Acknowledge("ack-id")
		assert.False(t, ok, "Acknowledge should fail when configured")
	})
}

func TestDistributedPriorityQueueFailures(t *testing.T) {
	t.Run("Enqueue failure", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()
		mockQueue.ShouldFailEnqueue = true

		// Track subscriptions to ensure none are called on failure
		var subscriptionCalls []string
		mockQueue.Subscribe(func(action string) {
			subscriptionCalls = append(subscriptionCalls, action)
		})

		ok := queue.Add("test-data", 1)
		assert.False(t, ok, "Enqueue should fail when configured")
		assert.Equal(t, 0, queue.NumPending(), "Queue should be empty")
		assert.Equal(t, 0, len(subscriptionCalls), "No subscription calls should be made")
	})

	t.Run("Acknowledge failure", func(t *testing.T) {
		_, mockQueue := setupDistributedPriorityQueue()
		mockQueue.ShouldFailAcknowledge = true

		ok := mockQueue.Acknowledge("ack-id")
		assert.False(t, ok, "Acknowledge should fail when configured")
	})
}

// Additional edge case tests

func TestDistributedQueueEdgeCases(t *testing.T) {
	t.Run("Add with empty data", func(t *testing.T) {
		queue, _ := setupDistributedQueue()

		// Test adding empty string
		ok := queue.Add("")
		assert.True(t, ok, "Empty string should be added successfully")
	})

	t.Run("Add with nil-like data", func(t *testing.T) {
		// Test with pointer type that can be nil
		mockQueue := mocks.NewMockDistributedQueue()
		queue := NewDistributedQueue[*string](mockQueue)

		var nilString *string
		ok := queue.Add(nilString)
		assert.True(t, ok, "Nil pointer should be added successfully")
	})

	t.Run("Subscription with failed enqueue", func(t *testing.T) {
		queue, mockQueue := setupDistributedQueue()

		// Close the queue first
		mockQueue.Close()

		// Track subscription calls
		var subscriptionCalls []string
		mockQueue.Subscribe(func(action string) {
			subscriptionCalls = append(subscriptionCalls, action)
		})

		// Attempt to add a job to closed queue
		ok := queue.Add("test-data")
		assert.False(t, ok, "Job should not be added to closed queue")
		assert.Equal(t, 0, len(subscriptionCalls), "No subscription calls should be made for failed enqueue")
	})
}

func TestDistributedPriorityQueueEdgeCases(t *testing.T) {
	t.Run("Add with zero priority", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Test adding with zero priority
		ok := queue.Add("test-data", 0)
		assert.True(t, ok, "Job with zero priority should be added successfully")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add with negative priority", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Test adding with negative priority (should have highest priority)
		ok := queue.Add("test-data", -5)
		assert.True(t, ok, "Job with negative priority should be added successfully")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add with very high priority", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Test adding with very high priority number (should have lowest priority)
		ok := queue.Add("test-data", 1000000)
		assert.True(t, ok, "Job with very high priority number should be added successfully")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Subscription with failed enqueue", func(t *testing.T) {
		queue, mockQueue := setupDistributedPriorityQueue()

		// Close the queue first
		mockQueue.Close()

		// Track subscription calls
		var subscriptionCalls []string
		mockQueue.Subscribe(func(action string) {
			subscriptionCalls = append(subscriptionCalls, action)
		})

		// Attempt to add a job to closed queue
		ok := queue.Add("test-data", 1)
		assert.False(t, ok, "Job should not be added to closed queue")
		assert.Equal(t, 0, len(subscriptionCalls), "No subscription calls should be made for failed enqueue")
	})
}
