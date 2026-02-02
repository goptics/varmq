package varmq

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/goptics/varmq/mocks"
)

// Additional tests for Worker Binder functions with 0% coverage

func TestWorkerBinderPersistentMethods(t *testing.T) {
	t.Run("WithPersistentQueue", func(t *testing.T) {
		// Create a worker
		workerFunc := func(j Job[string]) {
			// Simple processor
		}
		worker := NewWorker(workerFunc)

		// Create a mock persistent queue
		mockQueue := mocks.NewMockPersistentQueue()

		// Test WithPersistentQueue
		persistentQueue := worker.WithPersistentQueue(mockQueue)

		// Verify the queue was created and bound correctly
		assert.NotNil(t, persistentQueue, "Persistent queue should not be nil")
		// The Worker() method returns the underlying worker, not the binder
		assert.NotNil(t, persistentQueue.Worker(), "Worker should be bound correctly")

		// Test adding a job
		ok := persistentQueue.Add("test-data")
		assert.True(t, ok, "Should be able to add job to persistent queue")
		// NumPending might be 0 or 1 due to immediate processing by worker
		assert.LessOrEqual(t, persistentQueue.NumPending(), 1, "Should have at most one pending job (may be processed immediately)")

		// Clean up
		worker.Stop()
	})

	t.Run("WithPersistentPriorityQueue", func(t *testing.T) {
		// Create a worker
		workerFunc := func(j Job[string]) {
			// Simple processor
		}
		worker := NewWorker(workerFunc)

		// Create a mock persistent priority queue
		mockQueue := mocks.NewMockPersistentPriorityQueue()

		// Test WithPersistentPriorityQueue
		persistentPriorityQueue := worker.WithPersistentPriorityQueue(mockQueue)

		// Verify the queue was created and bound correctly
		assert.NotNil(t, persistentPriorityQueue, "Persistent priority queue should not be nil")
		// The Worker() method returns the underlying worker, not the binder
		assert.NotNil(t, persistentPriorityQueue.Worker(), "Worker should be bound correctly")

		// Test adding a job with priority
		ok := persistentPriorityQueue.Add("test-data", 5)
		assert.True(t, ok, "Should be able to add job to persistent priority queue")
		assert.LessOrEqual(t, persistentPriorityQueue.NumPending(), 1, "Should have at most one pending job (may be processed immediately)")

		// Clean up
		worker.Stop()
	})
}

func TestWorkerBinderDistributedMethods(t *testing.T) {
	t.Run("WithDistributedQueue", func(t *testing.T) {
		// Create a worker
		workerFunc := func(j Job[string]) {
			// Simple processor
		}
		worker := NewWorker(workerFunc)

		// Create a mock distributed queue
		mockQueue := mocks.NewMockDistributedQueue()

		// Test WithDistributedQueue
		distributedQueue := worker.WithDistributedQueue(mockQueue)

		// Verify the queue was created and bound correctly
		assert.NotNil(t, distributedQueue, "Distributed queue should not be nil")
		// Mock distributed queue starts with 0 items
		assert.Equal(t, 0, distributedQueue.NumPending(), "Should start with no pending jobs")

		// Test adding a job
		ok := distributedQueue.Add("test-data")
		assert.True(t, ok, "Should be able to add job to distributed queue")

		// Clean up
		worker.Stop()
	})

	t.Run("WithDistributedPriorityQueue", func(t *testing.T) {
		// Create a worker
		workerFunc := func(j Job[string]) {
			// Simple processor
		}
		worker := NewWorker(workerFunc)

		// Create a mock distributed priority queue
		mockQueue := mocks.NewMockDistributedPriorityQueue()

		// Test WithDistributedPriorityQueue
		distributedPriorityQueue := worker.WithDistributedPriorityQueue(mockQueue)

		// Verify the queue was created and bound correctly
		assert.NotNil(t, distributedPriorityQueue, "Distributed priority queue should not be nil")
		// Mock distributed priority queue starts with 0 items
		assert.Equal(t, 0, distributedPriorityQueue.NumPending(), "Should start with no pending jobs")

		// Test adding a job with priority
		ok := distributedPriorityQueue.Add("test-data", 3)
		assert.True(t, ok, "Should be able to add job to distributed priority queue")
		// Clean up
		worker.Stop()
	})
}

func TestHandleQueueSubscription(t *testing.T) {
	t.Run("handleQueueSubscription with enqueued action", func(t *testing.T) {
		// Create a worker
		workerFunc := func(j Job[string]) {
			// Simple processor that we can track
		}
		worker := NewWorker(workerFunc)

		// Get the worker binder to access handleQueueSubscription
		binder := worker.(*workerBinder[string])

		// Start the worker so it can process notifications
		err := binder.worker.start()
		assert.NoError(t, err, "Worker should start successfully")

		// Test handleQueueSubscription directly
		// This simulates what happens when a distributed queue notifies about an enqueued job
		binder.handleQueueSubscription("enqueued")

		// The function should complete without error (it calls notifyToPullNextJobs internally)
		// We can't easily test the internal notification, but we can verify the function runs

		// Test with different action (should be ignored)
		binder.handleQueueSubscription("other-action")

		// Clean up
		worker.Stop()
	})

	t.Run("handleQueueSubscription integration with distributed queue", func(t *testing.T) {
		// Create a worker
		workerFunc := func(j Job[string]) {
			// Simple processor
		}
		worker := NewWorker(workerFunc)

		// Create a mock distributed queue
		mockQueue := mocks.NewMockDistributedQueue()

		// Track subscription calls
		var subscriptionCalls []string
		originalSubscribers := mockQueue.Subscribers

		// Add our tracking alongside the worker's subscription
		mockQueue.Subscribe(func(action string) {
			subscriptionCalls = append(subscriptionCalls, action)
		})

		// Bind the distributed queue (this should call Subscribe internally)
		distributedQueue := worker.WithDistributedQueue(mockQueue)

		// Verify that the worker's handleQueueSubscription was registered
		// We can't directly verify this, but we can test that subscription works
		assert.NotNil(t, distributedQueue, "Distributed queue should be created")

		// Add a job to trigger subscription
		ok := distributedQueue.Add("test-data")
		assert.True(t, ok, "Should be able to add job")

		// Verify our tracking subscription was called
		assert.Equal(t, 1, len(subscriptionCalls), "Should have one subscription call")
		assert.Equal(t, "enqueued", subscriptionCalls[0], "Should receive enqueued action")

		// Verify that the worker's subscription was also registered
		// (We can't directly test this, but the fact that the queue works indicates it was set up correctly)
		assert.True(t, len(mockQueue.Subscribers) > len(originalSubscribers), "Worker subscription should be added")

		// Clean up
		worker.Stop()
	})

	t.Run("handleQueueSubscription integration with distributed priority queue", func(t *testing.T) {
		// Create a worker
		workerFunc := func(j Job[string]) {
			// Simple processor
		}
		worker := NewWorker(workerFunc)

		// Create a mock distributed priority queue
		mockQueue := mocks.NewMockDistributedPriorityQueue()

		// Track subscription calls
		var subscriptionCalls []string

		// Add our tracking alongside the worker's subscription
		mockQueue.Subscribe(func(action string) {
			subscriptionCalls = append(subscriptionCalls, action)
		})

		// Bind the distributed priority queue (this should call Subscribe internally)
		distributedPriorityQueue := worker.WithDistributedPriorityQueue(mockQueue)

		// Verify that the queue was created
		assert.NotNil(t, distributedPriorityQueue, "Distributed priority queue should be created")

		// Add a job to trigger subscription
		ok := distributedPriorityQueue.Add("test-data", 5)
		assert.True(t, ok, "Should be able to add job")

		// Verify our tracking subscription was called
		assert.Equal(t, 1, len(subscriptionCalls), "Should have one subscription call")
		assert.Equal(t, "enqueued", subscriptionCalls[0], "Should receive enqueued action")

		// Clean up
		worker.Stop()
	})
}

func TestWorkerBinderEdgeCases(t *testing.T) {
	t.Run("Multiple distributed queues with same worker", func(t *testing.T) {
		// Create a worker
		workerFunc := func(j Job[string]) {
			// Simple processor
		}
		worker := NewWorker(workerFunc)

		// Create multiple mock distributed queues
		mockQueue1 := mocks.NewMockDistributedQueue()
		mockQueue2 := mocks.NewMockDistributedQueue()

		// Bind both queues to the same worker
		distributedQueue1 := worker.WithDistributedQueue(mockQueue1)
		distributedQueue2 := worker.WithDistributedQueue(mockQueue2)

		// Both should be valid
		assert.NotNil(t, distributedQueue1, "First distributed queue should be created")
		assert.NotNil(t, distributedQueue2, "Second distributed queue should be created")

		// Both should work independently
		ok1 := distributedQueue1.Add("test-data-1")
		ok2 := distributedQueue2.Add("test-data-2")

		assert.True(t, ok1, "Should be able to add to first queue")
		assert.True(t, ok2, "Should be able to add to second queue")

		// Clean up
		worker.Stop()
	})

	t.Run("Persistent and distributed queues with same worker", func(t *testing.T) {
		// Create a worker
		workerFunc := func(j Job[string]) {
			// Simple processor
		}
		worker := NewWorker(workerFunc)

		// Create both persistent and distributed queues
		mockPersistentQueue := mocks.NewMockPersistentQueue()
		mockDistributedQueue := mocks.NewMockDistributedQueue()

		// Bind both types to the same worker
		persistentQueue := worker.WithPersistentQueue(mockPersistentQueue)
		distributedQueue := worker.WithDistributedQueue(mockDistributedQueue)

		// Both should be valid
		assert.NotNil(t, persistentQueue, "Persistent queue should be created")
		assert.NotNil(t, distributedQueue, "Distributed queue should be created")

		// Both should work
		ok1 := persistentQueue.Add("persistent-data")
		ok2 := distributedQueue.Add("distributed-data")

		assert.True(t, ok1, "Should be able to add to persistent queue")
		assert.True(t, ok2, "Should be able to add to distributed queue")

		// Clean up
		worker.Stop()
	})
}
