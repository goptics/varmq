package varmq

import (
	"errors"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goptics/varmq/internal/queues"
)

// Setup functions for each queue type
func setupBasicQueue() (*queue[string], *worker[string, iJob[string]], *queues.Queue[any]) {
	// Create a worker with a simple process function
	workerFunc := func(j iJob[string]) {
		// Simple processor that doesn't return anything
	}

	internalQueue := queues.NewQueue[any]()
	worker := newWorker(workerFunc, WithAutoRun(false))
	queue := newQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

func setupResultQueue() (*resultQueue[string, int], *worker[string, iResultJob[string, int]], *queues.Queue[any]) {
	// Create a worker with a simple process function that doubles an integer
	workerFunc := func(j iResultJob[string, int]) {
		val, err := strconv.Atoi(j.Data())
		if err != nil {
			j.sendError(err)
			return
		}

		j.sendResult(val * 2)
	}

	internalQueue := queues.NewQueue[any]()
	worker := newResultWorker(workerFunc, WithAutoRun(false))
	queue := newResultQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

func setupErrorQueue() (*errorQueue[string], *worker[string, iErrorJob[string]], *queues.Queue[any]) {
	// Create a worker with a simple process function that may return an error
	workerFunc := func(j iErrorJob[string]) {
		// Return error for specific input
		if j.Data() == "error" {
			j.sendError(errors.New("test error"))
		}
	}

	internalQueue := queues.NewQueue[any]()
	worker := newErrWorker(workerFunc, WithAutoRun(false))
	queue := newErrorQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

// Test groups for each queue type
func TestQueues(t *testing.T) {
	t.Run("BasicQueue", func(t *testing.T) {
		t.Run("Adding job to queue", func(t *testing.T) {
			queue, _, internalQueue := setupBasicQueue()

			// Test adding a job
			job, ok := queue.Add("test-data")
			assert.True(t, ok, "Job should be added successfully")
			assert.NotNil(t, job, "Job should not be nil")
			assert.Equal(t, 1, queue.Len(), "Queue should have one pending job")
			assert.Equal(t, 1, internalQueue.Len(), "Internal Queue should have one item")
		})

		t.Run("Processing job", func(t *testing.T) {
			queue, worker, internalQueue := setupBasicQueue()

			err := worker.Start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			job, _ := queue.Add("test-data")
			// Wait for job completion
			job.Wait()

			assert.Equal(t, 0, queue.Len(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
		})

		t.Run("Adding multiple jobs to queue", func(t *testing.T) {
			jobs := []Item[string]{
				{Data: "job1", ID: "1"},
				{Data: "job2", ID: "2"},
				{Data: "job3", ID: "3"},
			}

			queue, worker, internalQueue := setupBasicQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.Len()
			assert.Equal(t, 3, pending, "Queue should have three pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have three items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have three items")

			err := worker.Start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			// Wait for all jobs to complete
			groupJob.Wait()

			assert.Equal(t, 0, queue.Len(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
			assert.Equal(t, 0, groupJob.NumPending(), "Group job should have no pending jobs")
		})
	})

	t.Run("ResultQueue", func(t *testing.T) {
		t.Run("Adding job to queue", func(t *testing.T) {
			queue, _, internalQueue := setupResultQueue()

			// Test adding a job
			job, ok := queue.Add("42")
			assert.True(t, ok, "Job should be added successfully")
			assert.NotNil(t, job, "Job should not be nil")
			assert.Equal(t, 1, queue.Len(), "Queue should have one pending job")
			assert.Equal(t, 1, internalQueue.Len(), "Internal Queue should have one item")
		})

		t.Run("Processing job with result", func(t *testing.T) {
			queue, worker, internalQueue := setupResultQueue()
			worker.Start()
			defer worker.Stop()

			job, _ := queue.Add("42")
			result, err := job.Result()

			assert.NoError(t, err, "Job should complete without error")
			assert.Equal(t, 84, result, "Result should be double the input")
			assert.Equal(t, 0, queue.Len(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
		})

		t.Run("Processing job with error", func(t *testing.T) {
			queue, worker, _ := setupResultQueue()
			worker.Start()
			defer worker.Stop()

			job, _ := queue.Add("invalid")
			result, err := job.Result()

			assert.Error(t, err, "Job should return an error for invalid input")
			assert.Equal(t, 0, result, "Result should be zero for error case")
		})

		t.Run("Adding multiple jobs to queue", func(t *testing.T) {
			jobs := []Item[string]{
				{Data: "1", ID: "job1"},
				{Data: "2", ID: "job2"},
				{Data: "3", ID: "job3"},
				{Data: "4", ID: "job4"},
				{Data: "5", ID: "job5"},
			}

			queue, worker, internalQueue := setupResultQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.Len()
			assert.Equal(t, 5, pending, "Queue should have five pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have five items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have five items")

			worker.Start()
			defer worker.Stop()

			results := groupJob.Results()

			// Collect all results
			var got []int
			for r := range results {
				got = append(got, r.Data)
			}

			// Expect one result per job
			assert.Len(t, got, len(jobs))

			// Build expected set of doubled values
			expected := make(map[int]struct{}, len(jobs))
			for _, j := range jobs {
				v, _ := strconv.Atoi(j.Data)
				expected[v*2] = struct{}{}
			}

			// Verify each output is one of the expected doubles
			for _, val := range got {
				_, ok := expected[val]
				assert.True(t, ok, "unexpected result value: %d", val)
			}

			assert.Equal(t, 0, queue.Len(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
			assert.Equal(t, 0, groupJob.NumPending(), "Group job should have no pending jobs")
		})
	})

	t.Run("ErrorQueue", func(t *testing.T) {

		t.Run("Adding job to queue", func(t *testing.T) {
			queue, _, internalQueue := setupErrorQueue()

			// Test adding a job
			job, ok := queue.Add("test-data")
			assert.True(t, ok, "Job should be added successfully")
			assert.NotNil(t, job, "Job should not be nil")
			assert.Equal(t, 1, queue.Len(), "Queue should have one pending job")
			assert.Equal(t, 1, internalQueue.Len(), "Internal Queue should have one item")
		})

		t.Run("Processing job with success", func(t *testing.T) {
			queue, worker, internalQueue := setupErrorQueue()
			worker.Start()
			defer worker.Stop()

			job, _ := queue.Add("success")
			err := job.Err()

			assert.NoError(t, err, "Job should complete without error")
			assert.Equal(t, 0, queue.Len(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
		})

		t.Run("Processing job with error", func(t *testing.T) {
			queue, worker, _ := setupErrorQueue()
			worker.Start()
			defer worker.Stop()

			job, _ := queue.Add("error")
			err := job.Err()

			assert.Error(t, err, "Job should return an error")
			assert.Equal(t, "test error", err.Error(), "Error message should match expected")
		})

		t.Run("Adding multiple jobs to queue", func(t *testing.T) {
			jobs := []Item[string]{
				{Data: "success", ID: "job1"},
				{Data: "error", ID: "job2"},
				{Data: "success", ID: "job3"},
			}

			queue, worker, internalQueue := setupErrorQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.Len()
			assert.Equal(t, 3, pending, "Queue should have three pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have three items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have three items")

			worker.Start()
			defer worker.Stop()

			errors := groupJob.Errs()

			// Collect all errors
			errorCount := 0
			for err := range errors {
				if err != nil {
					errorCount++
					assert.Equal(t, "test error", err.Error(), "Error message should match expected")
				}
			}

			assert.Equal(t, 1, errorCount, "Should have exactly one error")
			assert.Equal(t, 0, queue.Len(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
			assert.Equal(t, 0, groupJob.NumPending(), "Group job should have no pending jobs")
		})
	})

}

// Setup functions for priority queues
func setupPriorityQueue() (*priorityQueue[string], *worker[string, iJob[string]], *queues.PriorityQueue[any]) {
	// Create a worker with a simple process function
	workerFunc := func(j iJob[string]) {
		// Simple processor that doesn't return anything
	}

	internalQueue := queues.NewPriorityQueue[any]()
	worker := newWorker(workerFunc, WithAutoRun(false))
	queue := newPriorityQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

func setupResultPriorityQueue() (*resultPriorityQueue[string, int], *worker[string, iResultJob[string, int]], *queues.PriorityQueue[any]) {
	// Create a worker with a simple process function that doubles an integer
	workerFunc := func(j iResultJob[string, int]) {
		val, err := strconv.Atoi(j.Data())
		if err != nil {
			j.sendError(err)
			return
		}

		j.sendResult(val * 2)
	}

	internalQueue := queues.NewPriorityQueue[any]()
	worker := newResultWorker(workerFunc, WithAutoRun(false))
	queue := newResultPriorityQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

func setupErrorPriorityQueue() (*errorPriorityQueue[string], *worker[string, iErrorJob[string]], *queues.PriorityQueue[any]) {
	// Create a worker with a simple process function that may return an error
	workerFunc := func(j iErrorJob[string]) {
		// Return error for specific input
		if j.Data() == "error" {
			j.sendError(errors.New("test error"))
		}
	}

	internalQueue := queues.NewPriorityQueue[any]()
	worker := newErrWorker(workerFunc, WithAutoRun(false))
	queue := newErrorPriorityQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

func TestPriorityQueues(t *testing.T) {
	// Test cases for Priority Queue
	t.Run("PriorityQueue", func(t *testing.T) {
		t.Run("Adding job to queue with priority", func(t *testing.T) {
			queue, _, internalQueue := setupPriorityQueue()

			// Test adding jobs with different priorities
			job1, ok := queue.Add("high-priority", 1)
			assert.True(t, ok, "Job should be added successfully")
			assert.NotNil(t, job1, "Job should not be nil")

			_, ok = queue.Add("medium-priority", 5)
			assert.True(t, ok, "Job should be added successfully")
			_, ok = queue.Add("low-priority", 10)
			assert.True(t, ok, "Job should be added successfully")

			assert.Equal(t, 3, queue.Len(), "Queue should have three pending jobs")
			assert.Equal(t, 3, internalQueue.Len(), "Internal Queue should have three items")
		})

		t.Run("Processing jobs with priority order", func(t *testing.T) {
			queue, worker, _ := setupPriorityQueue()

			worker.Start()
			defer worker.Stop()

			// Add jobs with different priorities (lower number = higher priority)
			job1, _ := queue.Add("high-priority", 1)
			job2, _ := queue.Add("medium-priority", 5)
			job3, _ := queue.Add("low-priority", 10)

			// Wait for all jobs to complete
			job1.Wait()
			job2.Wait()
			job3.Wait()

			assert.Equal(t, 0, queue.Len(), "Queue should have no pending jobs")
		})

		t.Run("Adding multiple jobs with priorities", func(t *testing.T) {
			jobs := []Item[string]{
				{Data: "high", ID: "job1", Priority: 1},
				{Data: "medium", ID: "job2", Priority: 5},
				{Data: "low", ID: "job3", Priority: 10},
			}

			queue, worker, internalQueue := setupPriorityQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.Len()
			assert.Equal(t, 3, pending, "Queue should have three pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have three items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have three items")

			worker.Start()
			defer worker.Stop()

			// Wait for all jobs to complete
			groupJob.Wait()

			assert.Equal(t, 0, queue.Len(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
			assert.Equal(t, 0, groupJob.NumPending(), "Group job should have no pending jobs")
		})
	})

	t.Run("ResultPriorityQueue", func(t *testing.T) {

		t.Run("Adding job to queue with priority", func(t *testing.T) {
			queue, _, internalQueue := setupResultPriorityQueue()

			// Test adding jobs with different priorities
			job1, ok := queue.Add("42", 1) // high priority
			assert.True(t, ok, "Job should be added successfully")
			assert.NotNil(t, job1, "Job should not be nil")

			_, ok = queue.Add("30", 5) // medium priority
			assert.True(t, ok, "Job should be added successfully")

			_, ok = queue.Add("10", 10) // low priority
			assert.True(t, ok, "Job should be added successfully")

			assert.Equal(t, 3, queue.Len(), "Queue should have three pending jobs")
			assert.Equal(t, 3, internalQueue.Len(), "Internal Queue should have three items")
		})

		t.Run("Processing job with result and priority", func(t *testing.T) {
			queue, worker, _ := setupResultPriorityQueue()

			worker.Start()
			defer worker.Stop()

			// Add jobs with different priorities (lower number = higher priority)
			job1, _ := queue.Add("42", 1)  // high priority
			job2, _ := queue.Add("30", 5)  // medium priority
			job3, _ := queue.Add("10", 10) // low priority

			// Check results in order of priority
			result1, err := job1.Result()
			assert.NoError(t, err, "High priority job should complete without error")
			assert.Equal(t, 84, result1, "Result should be double the input")

			result2, err := job2.Result()
			assert.NoError(t, err, "Medium priority job should complete without error")
			assert.Equal(t, 60, result2, "Result should be double the input")

			result3, err := job3.Result()
			assert.NoError(t, err, "Low priority job should complete without error")
			assert.Equal(t, 20, result3, "Result should be double the input")
		})

		t.Run("Processing job with error", func(t *testing.T) {
			queue, worker, _ := setupResultPriorityQueue()

			worker.Start()
			defer worker.Stop()

			job, _ := queue.Add("invalid", 1)
			result, err := job.Result()

			assert.Error(t, err, "Job should return an error for invalid input")
			assert.Equal(t, 0, result, "Result should be zero for error case")
		})

		t.Run("Adding multiple jobs with priorities", func(t *testing.T) {
			jobs := []Item[string]{
				{Data: "5", ID: "job1", Priority: 1},   // high priority
				{Data: "10", ID: "job2", Priority: 5},  // medium priority
				{Data: "15", ID: "job3", Priority: 10}, // low priority
			}

			queue, worker, internalQueue := setupResultPriorityQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.Len()
			assert.Equal(t, 3, pending, "Queue should have three pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have three items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have three items")

			worker.Start()
			defer worker.Stop()

			results := groupJob.Results()

			// Collect all results
			var got []int
			for result := range results {
				got = append(got, result.Data)
			}

			// We can't guarantee the order of results in the channel,
			// but we can check that all expected values are there
			expected := []int{10, 20, 30}
			assert.ElementsMatch(t, expected, got, "Results should match expected values")
			assert.Equal(t, 0, queue.Len(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
			assert.Equal(t, 0, groupJob.NumPending(), "Group job should have no pending jobs")
		})
	})

	t.Run("ErrorPriorityQueue", func(t *testing.T) {
		t.Run("Adding job to queue with priority", func(t *testing.T) {
			queue, _, internalQueue := setupErrorPriorityQueue()

			// Test adding jobs with different priorities
			job1, ok := queue.Add("success", 1) // high priority
			assert.True(t, ok, "Job should be added successfully")
			assert.NotNil(t, job1, "Job should not be nil")

			_, ok = queue.Add("error", 5) // medium priority
			assert.True(t, ok, "Job should be added successfully")

			_, ok = queue.Add("success", 10) // low priority
			assert.True(t, ok, "Job should be added successfully")

			assert.Equal(t, 3, queue.Len(), "Queue should have three pending jobs")
			assert.Equal(t, 3, internalQueue.Len(), "Internal Queue should have three items")
		})

		t.Run("Processing successful job", func(t *testing.T) {
			queue, worker, _ := setupErrorPriorityQueue()

			worker.Start()
			defer worker.Stop()

			job, _ := queue.Add("success", 1)
			err := job.Err()

			assert.NoError(t, err, "Job should complete without error")
		})

		t.Run("Processing job with error", func(t *testing.T) {
			queue, worker, _ := setupErrorPriorityQueue()

			worker.Start()
			defer worker.Stop()

			job, _ := queue.Add("error", 1)
			err := job.Err()

			assert.Error(t, err, "Job should return an error")
			assert.Equal(t, "test error", err.Error(), "Error message should match expected")
		})

		t.Run("Adding multiple jobs with priorities", func(t *testing.T) {
			jobs := []Item[string]{
				{Data: "success", ID: "job1", Priority: 1},  // high priority
				{Data: "error", ID: "job2", Priority: 5},    // medium priority
				{Data: "success", ID: "job3", Priority: 10}, // low priority
			}

			queue, worker, internalQueue := setupErrorPriorityQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.Len()
			assert.Equal(t, 3, pending, "Queue should have three pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have three items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have three items")

			worker.Start()
			defer worker.Stop()

			errors := groupJob.Errs()

			// Collect all errors
			errorCount := 0
			for err := range errors {
				if err != nil {
					errorCount++
					assert.Equal(t, "test error", err.Error(), "Error message should match expected")
				}
			}

			assert.Equal(t, 1, errorCount, "Should have exactly one error")
			assert.Equal(t, 0, queue.Len(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
			assert.Equal(t, 0, groupJob.NumPending(), "Group job should have no pending jobs")
		})
	})
}

func TestExternalQueue(t *testing.T) {
	t.Run("NumPending", func(t *testing.T) {
		queue, _, _ := setupBasicQueue()
		assert := assert.New(t)

		// Initially no pending jobs
		assert.Equal(0, queue.Len(), "Queue should have no pending jobs initially")

		// Add a job and check pending count
		queue.Add("test-data")
		assert.Equal(1, queue.Len(), "Queue should have one pending job after Add")

		// Add more jobs and check pending count
		queue.Add("test-data-2")
		queue.Add("test-data-3")
		assert.Equal(3, queue.Len(), "Queue should have three pending jobs after multiple Adds")
	})

	t.Run("Worker", func(t *testing.T) {
		queue, expectedWorker, _ := setupBasicQueue()
		assert := assert.New(t)

		// Test that Worker returns the expected worker
		actualWorker := queue.Worker()
		assert.Equal(expectedWorker, actualWorker, "Worker() should return the expected worker instance")
	})

	t.Run("Purge", func(t *testing.T) {
		queue, _, internalQueue := setupBasicQueue()
		assert := assert.New(t)

		// Add several jobs
		for i := range 5 {
			queue.Add("test-data-" + strconv.Itoa(i))
		}
		assert.LessOrEqual(queue.Len(), 5, "Queue should have five pending jobs")

		// Purge the queue
		queue.Purge()

		// After purging, should have no pending jobs
		assert.Equal(0, queue.Len(), "Queue should have no pending jobs after Purge")
		assert.Equal(0, internalQueue.Len(), "Internal queue should be empty after Purge")
	})

	t.Run("Close", func(t *testing.T) {
		queue, worker, _ := setupBasicQueue()
		assert := assert.New(t)

		worker.Start()
		defer worker.Stop()

		// Add several jobs
		for i := range 5 {
			queue.Add("test-data-" + strconv.Itoa(i))
		}
		assert.LessOrEqual(queue.Len(), 5, "Queue should have at most five pending jobs")

		// Close the queue
		err := queue.Close()
		assert.NoError(err, "Close should not return an error")

		_, ok := queue.Add("test-data-6")
		assert.False(ok, "Add should fail after Close")

		worker.Wait()
		assert.Equal(0, queue.Len(), "worker should process all pending jobs after close")
	})
}

func TestQueueCapacity(t *testing.T) {
	t.Run("BasicQueue", func(t *testing.T) {
		t.Run("Add returns false when at capacity", func(t *testing.T) {
			workerFunc := func(j iJob[string]) {}
			internalQueue := queues.NewQueue[any]()
			worker := newWorker(workerFunc, WithAutoRun(false))
			queue := newQueue(worker, internalQueue, WithQueueCapacity(2))

			_, ok1 := queue.Add("item-1")
			assert.True(t, ok1, "First item should be added successfully")

			_, ok2 := queue.Add("item-2")
			assert.True(t, ok2, "Second item should be added successfully")

			_, ok3 := queue.Add("item-3")
			assert.False(t, ok3, "Third item should fail when queue is at capacity")

			assert.Equal(t, 2, queue.Len(), "Queue should have exactly 2 pending items")
		})

		t.Run("AddAll skips items beyond capacity", func(t *testing.T) {
			workerFunc := func(j iJob[string]) {}
			internalQueue := queues.NewQueue[any]()
			worker := newWorker(workerFunc, WithAutoRun(false))

			queue := newQueue(worker, internalQueue, WithQueueCapacity(2))

			items := []Item[string]{
				{Data: "item-1", ID: "1"},
				{Data: "item-2", ID: "2"},
				{Data: "item-3", ID: "3"},
				{Data: "item-4", ID: "4"},
			}

			queue.AddAll(items)
			assert.Equal(t, 2, queue.Len(), "Queue should only accept up to capacity")
		})

		t.Run("Zero capacity means unlimited", func(t *testing.T) {
			workerFunc := func(j iJob[string]) {}
			internalQueue := queues.NewQueue[any]()
			worker := newWorker(workerFunc, WithAutoRun(false))
			queue := newQueue(worker, internalQueue, WithQueueCapacity(0))

			for i := range 100 {
				_, ok := queue.Add("item-" + strconv.Itoa(i))
				assert.True(t, ok, "Items should be added without limit")
			}
			assert.Equal(t, 100, queue.Len())
		})

		t.Run("No capacity config means unlimited", func(t *testing.T) {
			queue, _, _ := setupBasicQueue()

			for i := range 100 {
				_, ok := queue.Add("item-" + strconv.Itoa(i))
				assert.True(t, ok, "Items should be added without limit")
			}
			assert.Equal(t, 100, queue.Len())
		})

		t.Run("IsFull returns correct state", func(t *testing.T) {
			workerFunc := func(j iJob[string]) {}
			internalQueue := queues.NewQueue[any]()
			worker := newWorker(workerFunc, WithAutoRun(false))
			queue := newQueue(worker, internalQueue, WithQueueCapacity(2))

			assert.False(t, queue.IsFull(), "Empty queue should not be full")

			queue.Add("item-1")
			assert.False(t, queue.IsFull(), "Queue with 1/2 items should not be full")

			queue.Add("item-2")
			assert.True(t, queue.IsFull(), "Queue at capacity should be full")
		})

		t.Run("IsFull returns false with no capacity", func(t *testing.T) {
			queue, _, _ := setupBasicQueue()

			for i := range 100 {
				queue.Add("item-" + strconv.Itoa(i))
			}
			assert.False(t, queue.IsFull(), "Queue with no capacity should never be full")
		})
	})

	t.Run("PriorityQueue", func(t *testing.T) {
		t.Run("Add returns false when at capacity", func(t *testing.T) {
			workerFunc := func(j iJob[string]) {}
			internalQueue := queues.NewPriorityQueue[any]()
			worker := newWorker(workerFunc, WithAutoRun(false))
			queue := newPriorityQueue(worker, internalQueue, WithQueueCapacity(2))

			_, ok1 := queue.Add("high", 1)
			assert.True(t, ok1, "First item should be added successfully")

			_, ok2 := queue.Add("medium", 5)
			assert.True(t, ok2, "Second item should be added successfully")

			_, ok3 := queue.Add("low", 10)
			assert.False(t, ok3, "Third item should fail when queue is at capacity")

			assert.Equal(t, 2, queue.Len(), "Queue should have exactly 2 pending items")
		})

		t.Run("AddAll skips items beyond capacity", func(t *testing.T) {
			workerFunc := func(j iJob[string]) {}
			internalQueue := queues.NewPriorityQueue[any]()
			worker := newWorker(workerFunc, WithAutoRun(false))
			queue := newPriorityQueue(worker, internalQueue, WithQueueCapacity(2))

			items := []Item[string]{
				{Data: "high", ID: "1", Priority: 1},
				{Data: "medium", ID: "2", Priority: 5},
				{Data: "low", ID: "3", Priority: 10},
			}

			queue.AddAll(items)
			assert.Equal(t, 2, queue.Len(), "Queue should only accept up to capacity")
		})
	})

	t.Run("ResultQueue", func(t *testing.T) {
		t.Run("Add returns false when at capacity", func(t *testing.T) {
			workerFunc := func(j iResultJob[string, int]) {}
			internalQueue := queues.NewQueue[any]()
			worker := newResultWorker(workerFunc, WithAutoRun(false))
			queue := newResultQueue(worker, internalQueue, WithQueueCapacity(2))

			_, ok1 := queue.Add("item-1")
			assert.True(t, ok1, "First item should be added successfully")

			_, ok2 := queue.Add("item-2")
			assert.True(t, ok2, "Second item should be added successfully")

			_, ok3 := queue.Add("item-3")
			assert.False(t, ok3, "Third item should fail when queue is at capacity")

			assert.Equal(t, 2, queue.Len(), "Queue should have exactly 2 pending items")
		})

		t.Run("AddAll skips items beyond capacity", func(t *testing.T) {
			workerFunc := func(j iResultJob[string, int]) {}
			internalQueue := queues.NewQueue[any]()
			worker := newResultWorker(workerFunc, WithAutoRun(false))
			queue := newResultQueue(worker, internalQueue, WithQueueCapacity(2))

			items := []Item[string]{
				{Data: "item-1", ID: "1"},
				{Data: "item-2", ID: "2"},
				{Data: "item-3", ID: "3"},
				{Data: "item-4", ID: "4"},
			}

			queue.AddAll(items)
			assert.Equal(t, 2, queue.Len(), "Queue should only accept up to capacity")
		})
	})

	t.Run("ErrorQueue", func(t *testing.T) {
		t.Run("Add returns false when at capacity", func(t *testing.T) {
			workerFunc := func(j iErrorJob[string]) {}
			internalQueue := queues.NewQueue[any]()
			worker := newErrWorker(workerFunc, WithAutoRun(false))
			queue := newErrorQueue(worker, internalQueue, WithQueueCapacity(2))

			_, ok1 := queue.Add("item-1")
			assert.True(t, ok1, "First item should be added successfully")

			_, ok2 := queue.Add("item-2")
			assert.True(t, ok2, "Second item should be added successfully")

			_, ok3 := queue.Add("item-3")
			assert.False(t, ok3, "Third item should fail when queue is at capacity")

			assert.Equal(t, 2, queue.Len(), "Queue should have exactly 2 pending items")
		})

		t.Run("AddAll skips items beyond capacity", func(t *testing.T) {
			workerFunc := func(j iErrorJob[string]) {}
			internalQueue := queues.NewQueue[any]()
			worker := newErrWorker(workerFunc, WithAutoRun(false))
			queue := newErrorQueue(worker, internalQueue, WithQueueCapacity(2))

			items := []Item[string]{
				{Data: "item-1", ID: "1"},
				{Data: "item-2", ID: "2"},
				{Data: "item-3", ID: "3"},
				{Data: "item-4", ID: "4"},
			}

			queue.AddAll(items)
			assert.Equal(t, 2, queue.Len(), "Queue should only accept up to capacity")
		})
	})

	t.Run("ResultPriorityQueue", func(t *testing.T) {
		t.Run("Add returns false when at capacity", func(t *testing.T) {
			workerFunc := func(j iResultJob[string, int]) {}
			internalQueue := queues.NewPriorityQueue[any]()
			worker := newResultWorker(workerFunc, WithAutoRun(false))
			queue := newResultPriorityQueue(worker, internalQueue, WithQueueCapacity(2))

			_, ok1 := queue.Add("high", 1)
			assert.True(t, ok1, "First item should be added successfully")

			_, ok2 := queue.Add("medium", 5)
			assert.True(t, ok2, "Second item should be added successfully")

			_, ok3 := queue.Add("low", 10)
			assert.False(t, ok3, "Third item should fail when queue is at capacity")

			assert.Equal(t, 2, queue.Len(), "Queue should have exactly 2 pending items")
		})

		t.Run("AddAll skips items beyond capacity", func(t *testing.T) {
			workerFunc := func(j iResultJob[string, int]) {}
			internalQueue := queues.NewPriorityQueue[any]()
			worker := newResultWorker(workerFunc, WithAutoRun(false))
			queue := newResultPriorityQueue(worker, internalQueue, WithQueueCapacity(2))

			items := []Item[string]{
				{Data: "high", ID: "1", Priority: 1},
				{Data: "medium", ID: "2", Priority: 5},
				{Data: "low", ID: "3", Priority: 10},
			}

			queue.AddAll(items)
			assert.Equal(t, 2, queue.Len(), "Queue should only accept up to capacity")
		})
	})

	t.Run("ErrorPriorityQueue", func(t *testing.T) {
		t.Run("Add returns false when at capacity", func(t *testing.T) {
			workerFunc := func(j iErrorJob[string]) {}
			internalQueue := queues.NewPriorityQueue[any]()
			worker := newErrWorker(workerFunc, WithAutoRun(false))
			queue := newErrorPriorityQueue(worker, internalQueue, WithQueueCapacity(2))

			_, ok1 := queue.Add("high", 1)
			assert.True(t, ok1, "First item should be added successfully")

			_, ok2 := queue.Add("medium", 5)
			assert.True(t, ok2, "Second item should be added successfully")

			_, ok3 := queue.Add("low", 10)
			assert.False(t, ok3, "Third item should fail when queue is at capacity")

			assert.Equal(t, 2, queue.Len(), "Queue should have exactly 2 pending items")
		})

		t.Run("AddAll skips items beyond capacity", func(t *testing.T) {
			workerFunc := func(j iErrorJob[string]) {}
			internalQueue := queues.NewPriorityQueue[any]()
			worker := newErrWorker(workerFunc, WithAutoRun(false))
			queue := newErrorPriorityQueue(worker, internalQueue, WithQueueCapacity(2))

			items := []Item[string]{
				{Data: "high", ID: "1", Priority: 1},
				{Data: "medium", ID: "2", Priority: 5},
				{Data: "low", ID: "3", Priority: 10},
			}

			queue.AddAll(items)
			assert.Equal(t, 2, queue.Len(), "Queue should only accept up to capacity")
		})
	})
}

// Purge must also reach jobs a worker already took into its batch but has not
// started: with concurrency 1 and 40 jobs, one runs, 31 wait in the batch
// buffer and 8 stay in storage. A worker only batches once its jobs have
// measured cheap, so the measurement is seeded.
func TestPurgeCancelsBatchedJobs(t *testing.T) {
	waitDone := func(t *testing.T, w Worker) {
		t.Helper()
		done := make(chan struct{})
		go func() { w.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Wait did not return after Purge")
		}
	}

	t.Run("queue", func(t *testing.T) {
		var ran atomic.Int32
		started, release := make(chan struct{}, 1), make(chan struct{})
		w := NewWorker(func(j Job[int]) {
			ran.Add(1)
			started <- struct{}{}
			<-release
		}, WithConcurrency(1), WithAutoRun(false))
		w.(*workerBinder[int]).jobNanos.Store(1) // measured: cheap jobs batch
		q := w.BindQueue()

		jobs := make([]EnqueuedJob, 40)
		for i := range jobs {
			j, ok := q.Add(i)
			require.True(t, ok)
			jobs[i] = j
		}

		require.NoError(t, w.Start())
		<-started
		require.Equal(t, "Processing", jobs[0].Status())
		require.Equal(t, "Queued", jobs[1].Status(), "second job waits in the batch buffer")

		q.Purge()
		close(release)
		waitDone(t, w)

		assert.Equal(t, int32(1), ran.Load(), "only the job already running may run")
		for i, j := range jobs[1:] {
			j.Wait() // returns once closed
			assert.Equal(t, "Closed", j.Status(), "job %d", i+1)
		}
		assert.Equal(t, 0, w.NumPending())
		assert.Equal(t, 0, w.NumProcessing())

		// The queue still works after a Purge.
		j, ok := q.Add(99)
		require.True(t, ok)
		<-started
		j.Wait()
		assert.Equal(t, int32(2), ran.Load())
	})

	t.Run("topic", func(t *testing.T) {
		var ran atomic.Int32
		started, release := make(chan struct{}, 1), make(chan struct{})
		w := NewWorker(func(j Job[int]) {
			ran.Add(1)
			started <- struct{}{}
			<-release
		}, WithConcurrency(1), WithAutoRun(false))
		w.(*workerBinder[int]).jobNanos.Store(1) // measured: cheap jobs batch
		topic := NewTopic[int]()
		sub := w.WithTopic(topic)

		for i := range 40 {
			require.True(t, topic.Publish(i))
		}

		require.NoError(t, w.Start())
		<-started
		require.Equal(t, 39, w.NumPending())

		sub.Purge()
		close(release)
		waitDone(t, w)

		assert.Equal(t, int32(1), ran.Load(), "only the job already running may run")
		assert.Equal(t, 0, w.NumPending())
	})
}

// AddAll calls the worker's id generator only for items without an ID, so a
// stateful generator numbers the unnamed items densely. Pinned here because
// it differs from Add with WithJobId, which still consumes a generated id.
func TestAddAllIdGeneratorCadence(t *testing.T) {
	var next atomic.Int32
	var mu sync.Mutex
	var ids []string

	w := NewWorker(func(j Job[string]) {
		mu.Lock()
		ids = append(ids, j.ID())
		mu.Unlock()
	}, WithJobIdGenerator(func() string {
		return "n" + strconv.Itoa(int(next.Add(1)))
	}))
	defer w.StopAndWait()

	w.BindQueue().AddAll([]Item[string]{
		{ID: "a", Data: "x"},
		{Data: "x"},
		{ID: "b", Data: "x"},
		{Data: "x"},
	}).Wait()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"g:a", "g:n1", "g:b", "g:n2"}, ids)
	assert.Equal(t, int32(2), next.Load(), "generator runs once per unnamed item")
}

// Stop and Pause must not run the jobs a worker holds in its batch but has
// not started: the worker parks them, still Queued and counted by NumPending
// (not by the queue's Len, which counts storage), and hands them out first,
// in order, after a Restart or Resume. A worker only batches once its jobs
// have measured cheap, so the measurement is seeded.
func TestStopAndPauseParkBatchedJobs(t *testing.T) {
	type fixture struct {
		w       IWorkerBinder[int]
		started chan struct{}
		release chan struct{}
		order   func() []int
	}
	newFixture := func(t *testing.T) fixture {
		t.Helper()
		var mu sync.Mutex
		var order []int
		started, release := make(chan struct{}, 1), make(chan struct{})
		w := NewWorker(func(j Job[int]) {
			mu.Lock()
			order = append(order, j.Data())
			first := len(order) == 1
			mu.Unlock()
			if first {
				started <- struct{}{}
				<-release
			}
		}, WithConcurrency(1), WithAutoRun(false))
		w.(*workerBinder[int]).jobNanos.Store(1) // measured: cheap jobs batch
		return fixture{w, started, release, func() []int {
			mu.Lock()
			defer mu.Unlock()
			return append([]int(nil), order...)
		}}
	}
	seq := func(n int) []int {
		s := make([]int, n)
		for i := range s {
			s[i] = i
		}
		return s
	}
	addAll := func(t *testing.T, q Queue[int], n int) []EnqueuedJob {
		t.Helper()
		jobs := make([]EnqueuedJob, n)
		for i := range jobs {
			j, ok := q.Add(i)
			require.True(t, ok)
			jobs[i] = j
		}
		return jobs
	}

	t.Run("Stop, then Restart runs the rest in order", func(t *testing.T) {
		f := newFixture(t)
		q := f.w.BindQueue()
		jobs := addAll(t, q, 40)
		require.NoError(t, f.w.Start())
		<-f.started
		require.Equal(t, 8, q.Len(), "31 wait in the batch, 8 in storage")

		// Hold the running job a while: its cost must be measured over it
		// alone, not spread over the 31 jobs parked.
		heldFrom := time.Now()
		require.NoError(t, f.w.Stop())
		time.Sleep(20 * time.Millisecond)
		held := time.Since(heldFrom)
		close(f.release)
		f.w.WaitUntilStopped()

		assert.Equal(t, []int{0}, f.order(), "only the job already running ran")
		assert.Equal(t, 8, q.Len(), "storage untouched: the batch is parked on the worker")
		assert.Equal(t, 39, f.w.NumPending(), "parked jobs count as pending")
		assert.Equal(t, uint64(1), f.w.Metrics().Completed())
		for i, j := range jobs[1:] {
			assert.Equal(t, "Queued", j.Status(), "job %d", i+1)
		}
		assert.GreaterOrEqual(t, f.w.(*workerBinder[int]).jobNanos.Load(), int64(held),
			"job cost measured over the one job that ran, not the whole batch")

		require.NoError(t, f.w.Start())
		f.w.Wait()
		assert.Equal(t, seq(40), f.order(), "parked jobs first, then storage, in queue order")
		assert.Equal(t, 0, q.Len())
		assert.Equal(t, 0, f.w.NumPending())
		f.w.Stop()
	})

	t.Run("Pause, then Resume runs the rest in order", func(t *testing.T) {
		f := newFixture(t)
		q := f.w.BindQueue()
		addAll(t, q, 40)
		require.NoError(t, f.w.Start())
		<-f.started

		require.NoError(t, f.w.Pause())
		close(f.release)
		f.w.WaitUntilPaused()

		assert.Equal(t, []int{0}, f.order(), "only the job already running ran")
		assert.Equal(t, 8, q.Len())
		assert.Equal(t, 39, f.w.NumPending())
		assert.Equal(t, 0, f.w.NumProcessing())

		require.NoError(t, f.w.Resume())
		f.w.Wait()
		assert.Equal(t, seq(40), f.order(), "the rest ran in queue order")
		f.w.Stop()
	})

	t.Run("topic: Stop parks the jobs, Restart runs them in publish order", func(t *testing.T) {
		f := newFixture(t)
		topic := NewTopic[int]()
		sub := f.w.WithTopic(topic)
		for i := range 40 {
			require.True(t, topic.Publish(i))
		}
		require.NoError(t, f.w.Start())
		<-f.started
		require.Equal(t, 8, sub.Len())

		require.NoError(t, f.w.Stop())
		close(f.release)
		f.w.WaitUntilStopped()

		assert.Equal(t, []int{0}, f.order())
		assert.Equal(t, 8, sub.Len())
		assert.Equal(t, 39, f.w.NumPending())

		require.NoError(t, f.w.Start())
		f.w.Wait()
		assert.Equal(t, seq(40), f.order(), "the rest ran in publish order")
		f.w.Stop()
	})

	t.Run("Purge before the stop closes the batch instead of parking it", func(t *testing.T) {
		f := newFixture(t)
		q := f.w.BindQueue()
		addAll(t, q, 40)
		require.NoError(t, f.w.Start())
		<-f.started

		q.Purge() // bumps the generation: the batch is stale
		require.NoError(t, f.w.Stop())
		close(f.release)
		f.w.WaitUntilStopped()

		assert.Equal(t, []int{0}, f.order())
		assert.Equal(t, 0, q.Len())
		assert.Equal(t, 0, f.w.NumPending(), "nothing parked")
	})

	t.Run("Purge while paused closes the parked jobs at once", func(t *testing.T) {
		f := newFixture(t)
		q := f.w.BindQueue()
		jobs := addAll(t, q, 40)
		require.NoError(t, f.w.Start())
		<-f.started

		require.NoError(t, f.w.Pause())
		close(f.release)
		f.w.WaitUntilPaused()
		require.Equal(t, 39, f.w.NumPending())

		q.Purge()

		done := make(chan struct{})
		go func() {
			for _, j := range jobs[1:] {
				j.Wait()
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("parked jobs were not closed by Purge while paused")
		}
		for i, j := range jobs[1:] {
			assert.Equal(t, "Closed", j.Status(), "job %d", i+1)
		}
		assert.Equal(t, 0, f.w.NumPending())

		require.NoError(t, f.w.Resume())
		f.w.Wait()
		assert.Equal(t, []int{0}, f.order(), "nothing purged ran after Resume")
		f.w.Stop()
	})
}

// Batches parked by several workers at one Stop go back out in the order
// they were dispatched, whatever order the workers parked them in.
func TestParkedBatchesKeepDispatchOrder(t *testing.T) {
	var mu sync.Mutex
	ran := map[int]int{}
	started, gate := make(chan struct{}, 4), make(chan struct{})
	w := NewWorker(func(j Job[int]) {
		mu.Lock()
		ran[j.Data()]++
		mu.Unlock()
		select {
		case started <- struct{}{}:
		default:
		}
		<-gate
	}, WithConcurrency(4), WithAutoRun(false))
	inner := w.(*workerBinder[int])
	// Cheap enough to batch, slow enough that a goroutine per share is worth
	// spawning: four workers each take a batch.
	inner.jobNanos.Store(1_000)
	q := w.BindQueue()
	for i := range 120 {
		_, ok := q.Add(i)
		require.True(t, ok)
	}

	require.NoError(t, w.Start())
	for range 4 {
		<-started
	}
	require.NoError(t, w.Stop())
	close(gate)
	w.WaitUntilStopped()

	inner.parkMx.Lock()
	batches := len(inner.parked)
	inner.parkMx.Unlock()
	require.GreaterOrEqual(t, batches, 2, "several workers parked a batch")

	// Drain the way the event loop would, while stopped: the order it hands
	// parked jobs out in.
	var drained []int
	buf := make([]iJob[int], maxBatch)
	for {
		n, _ := inner.unpark(buf)
		if n == 0 {
			break
		}
		for _, j := range buf[:n] {
			drained = append(drained, j.Data())
		}
	}
	assert.True(t, slices.IsSorted(drained), "parked jobs come back in dispatch order: %v", drained)
	mu.Lock()
	assert.Equal(t, 120, len(ran)+len(drained)+q.Len(), "every job ran, was parked or is in storage")
	mu.Unlock()
}

// A queue bound to several workers shares one Purge state: a Purge through
// any handle closes the jobs another worker parked.
func TestPurgeReachesJobsParkedByAnotherWorker(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	var first atomic.Bool
	b := NewWorker(func(j Job[int]) {
		if first.CompareAndSwap(false, true) {
			started <- struct{}{}
			<-release
		}
	}, WithConcurrency(1), WithAutoRun(false))
	b.(*workerBinder[int]).jobNanos.Store(1) // measured: cheap jobs batch
	a := NewWorker(func(j Job[int]) {}, WithAutoRun(false))

	store := queues.NewQueue[any]()
	qa, qb := a.WithQueue(store), b.WithQueue(store)
	jobs := make([]EnqueuedJob, 40)
	for i := range jobs {
		j, ok := qb.Add(i)
		require.True(t, ok)
		jobs[i] = j
	}

	require.NoError(t, b.Start())
	<-started
	require.NoError(t, b.Pause())
	close(release)
	b.WaitUntilPaused()
	require.Equal(t, 39, b.NumPending(), "31 parked on b, 8 in storage")

	qa.Purge() // through the other worker's handle

	for i, j := range jobs[1:] {
		done := make(chan struct{})
		go func() { j.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("job %d was not closed by a Purge through another handle", i+1)
		}
		assert.Equal(t, "Closed", j.Status(), "job %d", i+1)
	}
	assert.Equal(t, 0, b.NumPending())
	b.Stop()
}

// The rest of Purge on parked jobs: through a topic subscription, while
// stopped, and what an Unsubscribe leaves.
func TestParkedJobsPurgeAndUnsubscribe(t *testing.T) {
	type fixture struct {
		w       IWorkerBinder[int]
		started chan struct{}
		release chan struct{}
		ran     *atomic.Int32
	}
	newFixture := func() fixture {
		var ran atomic.Int32
		started, release := make(chan struct{}, 1), make(chan struct{})
		w := NewWorker(func(j Job[int]) {
			if ran.Add(1) == 1 {
				started <- struct{}{}
				<-release
			}
		}, WithConcurrency(1), WithAutoRun(false))
		w.(*workerBinder[int]).jobNanos.Store(1) // measured: cheap jobs batch
		return fixture{w, started, release, &ran}
	}
	publish := func(t *testing.T, topic Topic[int], n int) {
		t.Helper()
		for i := range n {
			require.True(t, topic.Publish(i))
		}
	}

	t.Run("topic Purge while paused closes the parked jobs", func(t *testing.T) {
		f := newFixture()
		topic := NewTopic[int]()
		sub := f.w.WithTopic(topic)
		publish(t, topic, 40)
		require.NoError(t, f.w.Start())
		<-f.started
		require.NoError(t, f.w.Pause())
		close(f.release)
		f.w.WaitUntilPaused()
		require.Equal(t, 39, f.w.NumPending())

		sub.Purge()

		assert.Equal(t, 0, f.w.NumPending())
		require.NoError(t, f.w.Resume())
		f.w.Wait()
		assert.Equal(t, int32(1), f.ran.Load(), "nothing purged ran")
		f.w.Stop()
	})

	t.Run("queue Purge while stopped, then Restart runs nothing purged", func(t *testing.T) {
		f := newFixture()
		q := f.w.BindQueue()
		for i := range 40 {
			_, ok := q.Add(i)
			require.True(t, ok)
		}
		require.NoError(t, f.w.Start())
		<-f.started
		require.NoError(t, f.w.Stop())
		close(f.release)
		f.w.WaitUntilStopped()
		require.Equal(t, 39, f.w.NumPending())

		q.Purge()
		assert.Equal(t, 0, f.w.NumPending())

		require.NoError(t, f.w.Start())
		f.w.Wait()
		assert.Equal(t, int32(1), f.ran.Load())
		f.w.Stop()
	})

	t.Run("Unsubscribe while stopped: parked jobs were already taken and still run", func(t *testing.T) {
		f := newFixture()
		topic := NewTopic[int]()
		sub := f.w.WithTopic(topic)
		publish(t, topic, 40)
		require.NoError(t, f.w.Start())
		<-f.started
		require.NoError(t, f.w.Stop())
		close(f.release)
		f.w.WaitUntilStopped()

		sub.Unsubscribe()
		assert.Equal(t, 31, f.w.NumPending(), "the 8 unread payloads are dropped, the 31 parked jobs stay")

		require.NoError(t, f.w.Start())
		f.w.Wait()
		assert.Equal(t, int32(32), f.ran.Load(), "the job that ran plus the 31 parked")
		f.w.Stop()
	})
}
