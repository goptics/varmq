package varmq

import (
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

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
			worker := newWorker(workerFunc)
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
