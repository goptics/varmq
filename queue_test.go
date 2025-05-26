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
	workerFunc := func(data string) {
		// Simple processor that doesn't return anything
	}

	internalQueue := queues.NewQueue[any]()
	worker := newWorker(workerFunc)
	queue := newQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

func setupResultQueue() (*resultQueue[string, int], *worker[string, iResultJob[string, int]], *queues.Queue[any]) {
	// Create a worker with a simple process function that doubles an integer
	workerFunc := func(data string) (int, error) {
		// Simple processor that converts string to int and doubles it
		val, err := strconv.Atoi(data)
		if err != nil {
			return 0, err
		}
		return val * 2, nil
	}

	internalQueue := queues.NewQueue[any]()
	worker := newResultWorker(workerFunc)
	queue := newResultQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

func setupErrorQueue() (*errorQueue[string], *worker[string, iErrorJob[string]], *queues.Queue[any]) {
	// Create a worker with a simple process function that may return an error
	workerFunc := func(data string) error {
		// Return error for specific input
		if data == "error" {
			return errors.New("test error")
		}
		return nil
	}

	internalQueue := queues.NewQueue[any]()
	worker := newErrWorker(workerFunc)
	queue := newErrorQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

// Test groups for each queue type
func TestQueue(t *testing.T) {
	t.Run("BasicQueue", func(t *testing.T) {
		t.Run("Start worker", func(t *testing.T) {
			_, worker, _ := setupBasicQueue()
			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()
		})

		t.Run("Adding job to queue", func(t *testing.T) {
			queue, _, internalQueue := setupBasicQueue()

			// Test adding a job
			job, ok := queue.Add("test-data")
			assert.True(t, ok, "Job should be added successfully")
			assert.NotNil(t, job, "Job should not be nil")
			assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
			assert.Equal(t, 1, internalQueue.Len(), "Internal Queue should have one item")
		})

		t.Run("Processing job", func(t *testing.T) {
			queue, worker, internalQueue := setupBasicQueue()

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			job, _ := queue.Add("test-data")
			// Wait for job completion
			job.Wait()

			assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
		})

		t.Run("Adding multiple jobs to queue", func(t *testing.T) {
			jobs := []Item[string]{
				{Value: "job1", ID: "1"},
				{Value: "job2", ID: "2"},
				{Value: "job3", ID: "3"},
			}

			queue, worker, internalQueue := setupBasicQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.NumPending()
			assert.Equal(t, 3, pending, "Queue should have three pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have three items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have three items")

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			// Wait for all jobs to complete
			groupJob.Wait()

			assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
			assert.Equal(t, 0, groupJob.NumPending(), "Group job should have no pending jobs")
		})
	})

	t.Run("ResultQueue", func(t *testing.T) {
		t.Run("Start worker", func(t *testing.T) {
			_, worker, _ := setupResultQueue()
			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()
		})

		t.Run("Adding job to queue", func(t *testing.T) {
			queue, _, internalQueue := setupResultQueue()

			// Test adding a job
			job, ok := queue.Add("42")
			assert.True(t, ok, "Job should be added successfully")
			assert.NotNil(t, job, "Job should not be nil")
			assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
			assert.Equal(t, 1, internalQueue.Len(), "Internal Queue should have one item")
		})

		t.Run("Processing job with result", func(t *testing.T) {
			queue, worker, internalQueue := setupResultQueue()

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			job, _ := queue.Add("42")
			result, err := job.Result()

			assert.NoError(t, err, "Job should complete without error")
			assert.Equal(t, 84, result, "Result should be double the input")
			assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
		})

		t.Run("Processing job with error", func(t *testing.T) {
			queue, worker, _ := setupResultQueue()

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			job, _ := queue.Add("invalid")
			result, err := job.Result()

			assert.Error(t, err, "Job should return an error for invalid input")
			assert.Equal(t, 0, result, "Result should be zero for error case")
		})

		t.Run("Adding multiple jobs to queue", func(t *testing.T) {
			jobs := []Item[string]{
				{Value: "1", ID: "job1"},
				{Value: "2", ID: "job2"},
				{Value: "3", ID: "job3"},
				{Value: "4", ID: "job4"},
				{Value: "5", ID: "job5"},
			}

			queue, worker, internalQueue := setupResultQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.NumPending()
			assert.Equal(t, 5, pending, "Queue should have five pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have five items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have five items")

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			results, _ := groupJob.Results()
			_, err = groupJob.Results()
			assert.Error(t, err, "Results channel should not be accessible more than once")

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
				v, _ := strconv.Atoi(j.Value)
				expected[v*2] = struct{}{}
			}

			// Verify each output is one of the expected doubles
			for _, val := range got {
				_, ok := expected[val]
				assert.True(t, ok, "unexpected result value: %d", val)
			}

			assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
			assert.Equal(t, 0, groupJob.NumPending(), "Group job should have no pending jobs")
		})
	})

	t.Run("ErrorQueue", func(t *testing.T) {
		t.Run("Start worker", func(t *testing.T) {
			_, worker, _ := setupErrorQueue()
			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()
		})

		t.Run("Adding job to queue", func(t *testing.T) {
			queue, _, internalQueue := setupErrorQueue()

			// Test adding a job
			job, ok := queue.Add("test-data")
			assert.True(t, ok, "Job should be added successfully")
			assert.NotNil(t, job, "Job should not be nil")
			assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
			assert.Equal(t, 1, internalQueue.Len(), "Internal Queue should have one item")
		})

		t.Run("Processing job with success", func(t *testing.T) {
			queue, worker, internalQueue := setupErrorQueue()

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			job, _ := queue.Add("success")
			err = job.Err()

			assert.NoError(t, err, "Job should complete without error")
			assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
		})

		t.Run("Processing job with error", func(t *testing.T) {
			queue, worker, _ := setupErrorQueue()

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			job, _ := queue.Add("error")
			err = job.Err()

			assert.Error(t, err, "Job should return an error")
			assert.Equal(t, "test error", err.Error(), "Error message should match expected")
		})

		t.Run("Adding multiple jobs to queue", func(t *testing.T) {
			jobs := []Item[string]{
				{Value: "success", ID: "job1"},
				{Value: "error", ID: "job2"},
				{Value: "success", ID: "job3"},
			}

			queue, worker, internalQueue := setupErrorQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.NumPending()
			assert.Equal(t, 3, pending, "Queue should have three pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have three items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have three items")

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			errors, err := groupJob.Errs()
			assert.NoError(t, err, "Getting errors channel should not fail")

			// Collect all errors
			errorCount := 0
			for err := range errors {
				if err != nil {
					errorCount++
					assert.Equal(t, "test error", err.Error(), "Error message should match expected")
				}
			}

			assert.Equal(t, 1, errorCount, "Should have exactly one error")
			assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
			assert.Equal(t, 0, groupJob.NumPending(), "Group job should have no pending jobs")
		})
	})

}

// Setup functions for priority queues
func setupPriorityQueue() (*priorityQueue[string], *worker[string, iJob[string]], *queues.PriorityQueue[any]) {
	// Create a worker with a simple process function
	workerFunc := func(data string) {
		// Simple processor that doesn't return anything
	}

	internalQueue := queues.NewPriorityQueue[any]()
	worker := newWorker(workerFunc)
	queue := newPriorityQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

func setupResultPriorityQueue() (*resultPriorityQueue[string, int], *worker[string, iResultJob[string, int]], *queues.PriorityQueue[any]) {
	// Create a worker with a simple process function that doubles an integer
	workerFunc := func(data string) (int, error) {
		// Simple processor that converts string to int and doubles it
		val, err := strconv.Atoi(data)
		if err != nil {
			return 0, err
		}
		return val * 2, nil
	}

	internalQueue := queues.NewPriorityQueue[any]()
	worker := newResultWorker(workerFunc)
	queue := newResultPriorityQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

func setupErrorPriorityQueue() (*errorPriorityQueue[string], *worker[string, iErrorJob[string]], *queues.PriorityQueue[any]) {
	// Create a worker with a simple process function that may return an error
	workerFunc := func(data string) error {
		// Return error for specific input
		if data == "error" {
			return errors.New("test error")
		}
		return nil
	}

	internalQueue := queues.NewPriorityQueue[any]()
	worker := newErrWorker(workerFunc)
	queue := newErrorPriorityQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

func TestPriorityQueue(t *testing.T) {
	// Test cases for Priority Queue
	t.Run("PriorityQueue", func(t *testing.T) {
		t.Run("Start worker", func(t *testing.T) {
			_, worker, _ := setupPriorityQueue()
			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()
		})

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

			assert.Equal(t, 3, queue.NumPending(), "Queue should have three pending jobs")
			assert.Equal(t, 3, internalQueue.Len(), "Internal Queue should have three items")
		})

		t.Run("Processing jobs with priority order", func(t *testing.T) {
			queue, worker, _ := setupPriorityQueue()

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			// Add jobs with different priorities (lower number = higher priority)
			job1, _ := queue.Add("high-priority", 1)
			job2, _ := queue.Add("medium-priority", 5)
			job3, _ := queue.Add("low-priority", 10)

			// Wait for all jobs to complete
			job1.Wait()
			job2.Wait()
			job3.Wait()

			assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
		})

		t.Run("Adding multiple jobs with priorities", func(t *testing.T) {
			jobs := []Item[string]{
				{Value: "high", ID: "job1", Priority: 1},
				{Value: "medium", ID: "job2", Priority: 5},
				{Value: "low", ID: "job3", Priority: 10},
			}

			queue, worker, internalQueue := setupPriorityQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.NumPending()
			assert.Equal(t, 3, pending, "Queue should have three pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have three items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have three items")

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			// Wait for all jobs to complete
			groupJob.Wait()

			assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
			assert.Equal(t, 0, groupJob.NumPending(), "Group job should have no pending jobs")
		})
	})

	t.Run("ResultPriorityQueue", func(t *testing.T) {
		t.Run("Start worker", func(t *testing.T) {
			_, worker, _ := setupResultPriorityQueue()
			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()
		})

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

			assert.Equal(t, 3, queue.NumPending(), "Queue should have three pending jobs")
			assert.Equal(t, 3, internalQueue.Len(), "Internal Queue should have three items")
		})

		t.Run("Processing job with result and priority", func(t *testing.T) {
			queue, worker, _ := setupResultPriorityQueue()

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
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

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			job, _ := queue.Add("invalid", 1)
			result, err := job.Result()

			assert.Error(t, err, "Job should return an error for invalid input")
			assert.Equal(t, 0, result, "Result should be zero for error case")
		})

		t.Run("Adding multiple jobs with priorities", func(t *testing.T) {
			jobs := []Item[string]{
				{Value: "5", ID: "job1", Priority: 1},   // high priority
				{Value: "10", ID: "job2", Priority: 5},  // medium priority
				{Value: "15", ID: "job3", Priority: 10}, // low priority
			}

			queue, worker, internalQueue := setupResultPriorityQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.NumPending()
			assert.Equal(t, 3, pending, "Queue should have three pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have three items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have three items")

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			results, _ := groupJob.Results()

			// Collect all results
			var got []int
			for result := range results {
				got = append(got, result.Data)
			}

			// We can't guarantee the order of results in the channel,
			// but we can check that all expected values are there
			expected := []int{10, 20, 30}
			assert.ElementsMatch(t, expected, got, "Results should match expected values")
			assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
			assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
			assert.Equal(t, 0, groupJob.NumPending(), "Group job should have no pending jobs")
		})
	})

	t.Run("ErrorPriorityQueue", func(t *testing.T) {
		t.Run("Start worker", func(t *testing.T) {
			_, worker, _ := setupErrorPriorityQueue()
			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()
		})

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

			assert.Equal(t, 3, queue.NumPending(), "Queue should have three pending jobs")
			assert.Equal(t, 3, internalQueue.Len(), "Internal Queue should have three items")
		})

		t.Run("Processing successful job", func(t *testing.T) {
			queue, worker, _ := setupErrorPriorityQueue()

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			job, _ := queue.Add("success", 1)
			err = job.Err()

			assert.NoError(t, err, "Job should complete without error")
		})

		t.Run("Processing job with error", func(t *testing.T) {
			queue, worker, _ := setupErrorPriorityQueue()

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			job, _ := queue.Add("error", 1)
			err = job.Err()

			assert.Error(t, err, "Job should return an error")
			assert.Equal(t, "test error", err.Error(), "Error message should match expected")
		})

		t.Run("Adding multiple jobs with priorities", func(t *testing.T) {
			jobs := []Item[string]{
				{Value: "success", ID: "job1", Priority: 1},  // high priority
				{Value: "error", ID: "job2", Priority: 5},    // medium priority
				{Value: "success", ID: "job3", Priority: 10}, // low priority
			}

			queue, worker, internalQueue := setupErrorPriorityQueue()

			groupJob := queue.AddAll(jobs)
			pending := queue.NumPending()
			assert.Equal(t, 3, pending, "Queue should have three pending jobs")
			assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have three items")
			assert.Equal(t, pending, groupJob.NumPending(), "Group job should have three items")

			err := worker.start()
			assert.NoError(t, err, "Worker should start successfully")
			defer worker.Stop()

			errors, err := groupJob.Errs()
			assert.NoError(t, err, "Getting errors channel should not fail")

			// Collect all errors
			errorCount := 0
			for err := range errors {
				if err != nil {
					errorCount++
					assert.Equal(t, "test error", err.Error(), "Error message should match expected")
				}
			}

			assert.Equal(t, 1, errorCount, "Should have exactly one error")
			assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
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
		assert.Equal(0, queue.NumPending(), "Queue should have no pending jobs initially")

		// Add a job and check pending count
		queue.Add("test-data")
		assert.Equal(1, queue.NumPending(), "Queue should have one pending job after Add")

		// Add more jobs and check pending count
		queue.Add("test-data-2")
		queue.Add("test-data-3")
		assert.Equal(3, queue.NumPending(), "Queue should have three pending jobs after multiple Adds")
	})

	t.Run("Worker", func(t *testing.T) {
		queue, expectedWorker, _ := setupBasicQueue()
		assert := assert.New(t)

		// Test that Worker returns the expected worker
		actualWorker := queue.Worker()
		assert.Equal(expectedWorker, actualWorker, "Worker() should return the expected worker instance")
	})

	t.Run("WaitUntilFinished", func(t *testing.T) {
		queue, worker, internalQueue := setupBasicQueue()
		assert := assert.New(t)

		// Start the worker
		err := worker.start()
		assert.NoError(err, "Worker should start successfully")
		defer worker.Stop()

		// Add several jobs
		for i := range 5 {
			queue.Add("test-data-" + strconv.Itoa(i))
		}
		assert.LessOrEqual(queue.NumPending(), 5, "Queue should have at most five pending jobs")

		// Wait until all jobs are processed
		queue.WaitUntilFinished()

		// After waiting, should have no pending jobs
		assert.Equal(0, queue.NumPending(), "Queue should have no pending jobs after WaitUntilFinished")
		assert.Equal(0, internalQueue.Len(), "Internal queue should be empty after WaitUntilFinished")
	})

	t.Run("Purge", func(t *testing.T) {
		queue, _, internalQueue := setupBasicQueue()
		assert := assert.New(t)

		// Add several jobs
		for i := range 5 {
			queue.Add("test-data-" + strconv.Itoa(i))
		}
		assert.LessOrEqual(queue.NumPending(), 5, "Queue should have five pending jobs")

		// Purge the queue
		queue.Purge()

		// After purging, should have no pending jobs
		assert.Equal(0, queue.NumPending(), "Queue should have no pending jobs after Purge")
		assert.Equal(0, internalQueue.Len(), "Internal queue should be empty after Purge")
	})

	t.Run("Close", func(t *testing.T) {
		queue, worker, internalQueue := setupBasicQueue()
		assert := assert.New(t)

		// Start the worker
		err := worker.start()
		assert.NoError(err, "Worker should start successfully")

		// Add several jobs
		for i := range 5 {
			queue.Add("test-data-" + strconv.Itoa(i))
		}
		assert.LessOrEqual(queue.NumPending(), 5, "Queue should have at most five pending jobs")

		// Close the queue
		err = queue.Close()
		assert.NoError(err, "Close should not return an error")

		// After closing, should have no pending jobs and worker should be stopped
		assert.Equal(0, queue.NumPending(), "Queue should have no pending jobs after Close")
		assert.Equal(0, internalQueue.Len(), "Internal queue should be empty after Close")
		assert.True(worker.IsStopped(), "Worker should be stopped after Close")
	})

	t.Run("WaitAndClose", func(t *testing.T) {
		queue, worker, internalQueue := setupBasicQueue()
		assert := assert.New(t)

		// Start the worker
		err := worker.start()
		assert.NoError(err, "Worker should start successfully")

		// Add several jobs
		for i := range 5 {
			queue.Add("test-data-" + strconv.Itoa(i))
		}
		assert.LessOrEqual(queue.NumPending(), 5, "Queue should have at most five pending jobs")

		// Wait and close the queue
		err = queue.WaitAndClose()
		assert.NoError(err, "WaitAndClose should not return an error")

		// After waiting and closing, should have no pending jobs and worker should be stopped
		assert.Equal(0, queue.NumPending(), "Queue should have no pending jobs after WaitAndClose")
		assert.Equal(0, internalQueue.Len(), "Internal queue should be empty after WaitAndClose")
		assert.True(worker.IsStopped(), "Worker should be stopped after WaitAndClose")
	})

	// t.Run("Resume paused worker during WaitUntilFinished", func(t *testing.T) {
	// 	queue, worker, internalQueue := setupBasicQueue()
	// 	assert := assert.New(t)

	// 	// Start the worker
	// 	err := worker.start()
	// 	assert.NoError(err, "Worker should start successfully")

	// 	// Pause the worker
	// 	err = worker.Pause()
	// 	assert.NoError(err, "Worker should pause successfully")
	// 	assert.True(worker.IsPaused(), "Worker should be paused")

	// 	// Add jobs while paused
	// 	for i := range 3 {
	// 		queue.Add("test-data-" + strconv.Itoa(i))
	// 	}
	// 	assert.Equal(3, queue.NumPending(), "Queue should have three pending jobs")

	// 	// WaitUntilFinished should automatically resume the worker
	// 	queue.WaitUntilFinished()

	// 	// After waiting, should have no pending jobs
	// 	assert.Equal(0, queue.NumPending(), "Queue should have no pending jobs after WaitUntilFinished")
	// 	assert.Equal(0, internalQueue.Len(), "Internal queue should be empty after WaitUntilFinished")
	// 	assert.False(worker.IsPaused(), "Worker should no longer be paused")

	// 	// Clean up
	// 	worker.Stop()
	// })
}
