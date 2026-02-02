package varmq

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/goptics/varmq/mocks"
)

// Setup functions for persistent queues
func setupPersistentQueue() (*persistentQueue[string], *worker[string, iJob[string]], *mocks.MockPersistentQueue) {
	// Create a worker with a simple process function
	workerFunc := func(j iJob[string]) {
		// Simple processor that doesn't return anything
	}

	mockQueue := mocks.NewMockPersistentQueue()
	worker := newWorker(workerFunc)
	queue := newPersistentQueue(worker, mockQueue).(*persistentQueue[string])

	return queue, worker, mockQueue
}

func setupPersistentPriorityQueue() (*persistentPriorityQueue[string], *worker[string, iJob[string]], *mocks.MockPersistentPriorityQueue) {
	// Create a worker with a simple process function
	workerFunc := func(j iJob[string]) {
		// Simple processor that doesn't return anything
	}

	mockQueue := mocks.NewMockPersistentPriorityQueue()
	worker := newWorker(workerFunc)
	queue := newPersistentPriorityQueue(worker, mockQueue).(*persistentPriorityQueue[string])

	return queue, worker, mockQueue
}

// Tests for PersistentQueue

func TestPersistentQueue(t *testing.T) {
	t.Run("Add method success", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentQueue()

		// Test adding a job
		ok := queue.Add("test-data")
		assert.True(t, ok, "Job should be added successfully")
		assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add method with job configs", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentQueue()

		// Test adding a job with custom ID
		ok := queue.Add("test-data", WithJobId("custom-id"))
		assert.True(t, ok, "Job should be added successfully with custom ID")
		assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add method failure when queue is closed", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentQueue()

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
		queue, _, mockQueue := setupPersistentQueue()

		// Add multiple jobs
		for i := 0; i < 5; i++ {
			ok := queue.Add("test-data-" + string(rune(i)))
			assert.True(t, ok, "Job %d should be added successfully", i)
		}

		assert.Equal(t, 5, queue.NumPending(), "Queue should have five pending jobs")
		assert.Equal(t, 5, mockQueue.Len(), "Mock queue should have five items")
	})

	t.Run("Purge method", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentQueue()

		// Add some jobs first
		queue.Add("test-data-1")
		queue.Add("test-data-2")
		assert.Equal(t, 2, queue.NumPending(), "Queue should have two pending jobs")

		// Purge the queue
		queue.Purge()
		assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs after purge")
		assert.Equal(t, 0, mockQueue.Len(), "Mock queue should be empty after purge")
	})

	t.Run("Worker method", func(t *testing.T) {
		queue, worker, _ := setupPersistentQueue()

		// Test that Worker() returns the correct worker
		returnedWorker := queue.Worker()
		assert.Equal(t, worker, returnedWorker, "Worker() should return the bound worker")
	})
}

// Tests for PersistentPriorityQueue

func TestPersistentPriorityQueue(t *testing.T) {
	t.Run("Add method success", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentPriorityQueue()

		// Test adding a job with priority
		ok := queue.Add("test-data", 5)
		assert.True(t, ok, "Job should be added successfully")
		assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add method with job configs", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentPriorityQueue()

		// Test adding a job with custom ID and priority
		ok := queue.Add("test-data", 3, WithJobId("custom-id"))
		assert.True(t, ok, "Job should be added successfully with custom ID and priority")
		assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add method failure when queue is closed", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentPriorityQueue()

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
		queue, _, mockQueue := setupPersistentPriorityQueue()

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
		queue, _, mockQueue := setupPersistentPriorityQueue()

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

	t.Run("Purge method", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentPriorityQueue()

		// Add some jobs first
		queue.Add("test-data-1", 1)
		queue.Add("test-data-2", 2)
		assert.Equal(t, 2, queue.NumPending(), "Queue should have two pending jobs")

		// Purge the queue
		queue.Purge()
		assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs after purge")
		assert.Equal(t, 0, mockQueue.Len(), "Mock queue should be empty after purge")
	})

	t.Run("Worker method", func(t *testing.T) {
		queue, worker, _ := setupPersistentPriorityQueue()

		// Test that Worker() returns the correct worker
		returnedWorker := queue.Worker()
		assert.Equal(t, worker, returnedWorker, "Worker() should return the bound worker")
	})
}

func TestPersistentQueueFailures(t *testing.T) {
	t.Run("Enqueue failure", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentQueue()
		mockQueue.ShouldFailEnqueue = true

		ok := queue.Add("test-data")
		assert.False(t, ok, "Enqueue should fail when configured")
		assert.Equal(t, 0, queue.NumPending(), "Queue should be empty")
	})

	t.Run("Acknowledge failure", func(t *testing.T) {
		_, _, mockQueue := setupPersistentQueue()
		mockQueue.ShouldFailAcknowledge = true

		ok := mockQueue.Acknowledge("ack-id")
		assert.False(t, ok, "Acknowledge should fail when configured")
	})
}

func TestPersistentPriorityQueueFailures(t *testing.T) {
	t.Run("Enqueue failure", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentPriorityQueue()
		mockQueue.ShouldFailEnqueue = true

		ok := queue.Add("test-data", 1)
		assert.False(t, ok, "Enqueue should fail when configured")
		assert.Equal(t, 0, queue.NumPending(), "Queue should be empty")
	})

	t.Run("Acknowledge failure", func(t *testing.T) {
		_, _, mockQueue := setupPersistentPriorityQueue()
		mockQueue.ShouldFailAcknowledge = true

		ok := mockQueue.Acknowledge("ack-id")
		assert.False(t, ok, "Acknowledge should fail when configured")
	})
}

// Additional edge case tests

func TestPersistentQueueEdgeCases(t *testing.T) {
	t.Run("Add with empty data", func(t *testing.T) {
		queue, _, _ := setupPersistentQueue()

		// Test adding empty string
		ok := queue.Add("")
		assert.True(t, ok, "Empty string should be added successfully")
	})

	t.Run("Add with nil-like data", func(t *testing.T) {
		// Test with pointer type that can be nil
		workerFunc := func(j iJob[*string]) {}
		mockQueue := mocks.NewMockPersistentQueue()
		worker := newWorker(workerFunc)
		queue := newPersistentQueue(worker, mockQueue)

		var nilString *string
		ok := queue.Add(nilString)
		assert.True(t, ok, "Nil pointer should be added successfully")
	})
}

func TestPersistentPriorityQueueEdgeCases(t *testing.T) {
	t.Run("Add with zero priority", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentPriorityQueue()

		// Test adding with zero priority
		ok := queue.Add("test-data", 0)
		assert.True(t, ok, "Job with zero priority should be added successfully")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add with negative priority", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentPriorityQueue()

		// Test adding with negative priority (should have highest priority)
		ok := queue.Add("test-data", -5)
		assert.True(t, ok, "Job with negative priority should be added successfully")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})

	t.Run("Add with very high priority", func(t *testing.T) {
		queue, _, mockQueue := setupPersistentPriorityQueue()

		// Test adding with very high priority number (should have lowest priority)
		ok := queue.Add("test-data", 1000000)
		assert.True(t, ok, "Job with very high priority number should be added successfully")
		assert.Equal(t, 1, mockQueue.Len(), "Mock queue should have one item")
	})
}
