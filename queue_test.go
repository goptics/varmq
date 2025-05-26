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
