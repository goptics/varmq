package varmq

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/goptics/varmq/internal/collections"
)

func initQueue() (*queue[string, int], *worker[string, int], *collections.Queue[any]) {
	// Create a worker with a simple process function that doubles an integer
	var workerFunc WorkerFunc[string, int]

	workerFunc = func(data string) (int, error) {
		// Simple processor that converts string to int and doubles it
		val, err := strconv.Atoi(data)
		if err != nil {
			return 0, err
		}
		return val * 2, nil
	}

	// Create an internal queue from collections and bind the worker to it
	internalQueue := collections.NewQueue[any]()
	worker := newWorker[string, int](workerFunc)
	queue := newQueue(worker, internalQueue)

	return queue, worker, internalQueue
}

// TestQueueAdd tests the Add method of the queue
func TestQueue(t *testing.T) {
	t.Run("Start worker", func(t *testing.T) {
		_, worker, _ := initQueue()
		err := worker.start()
		assert.Nil(t, err, "Worker should start successfully")
	})

	t.Run("Adding job to queue", func(t *testing.T) {
		queue, worker, internalQueue := initQueue()

		err := worker.start()
		assert.Nil(t, err, "Worker should start successfully")

		// Test adding a job
		job, ok := queue.Add("42")
		assert.True(t, ok, "Job should be added successfully")
		assert.NotNil(t, job, "Job should not be nil")
		assert.Equal(t, 1, queue.PendingCount(), "Queue should have one pending job")
		assert.Equal(t, 1, internalQueue.Len(), "Internal Queue should have one item")
	})

	t.Run("Processing job", func(t *testing.T) {
		queue, worker, internalQueue := initQueue()

		err := worker.start()
		assert.Nil(t, err, "Worker should start successfully")

		job, _ := queue.Add("42")
		job.Result()
		assert.Equal(t, 0, queue.PendingCount(), "Queue should have no pending jobs")
		assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
	})

	t.Run("Adding multiple jobs to queue", func(t *testing.T) {
		jobs := []Item[string]{
			{Value: "1", ID: "job1"},
			{Value: "2", ID: "job2"},
			{Value: "3", ID: "job3"},
			{Value: "4", ID: "job4"},
			{Value: "5", ID: "job5"},
		}

		queue, worker, internalQueue := initQueue()

		err := worker.start()
		assert.Nil(t, err, "Worker should start successfully")

		groupJob := queue.AddAll(jobs)
		assert.Equal(t, 5, queue.PendingCount(), "Queue should have three pending jobs")
		assert.Equal(t, 5, internalQueue.Len(), "Internal Queue should have three items")
		assert.Equal(t, 5, groupJob.Len(), "Internal Queue should have three items")

		results, _ := groupJob.Results()
		_, err = groupJob.Results()
		assert.NotNil(t, err, "Results should not be nil")

		count := 0
		for range results {
			count++
		}

		assert.Equal(t, 5, count, "Results should have three items")
		assert.Equal(t, 0, queue.PendingCount(), "Queue should have no pending jobs")
		assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
		assert.Equal(t, 0, groupJob.Len(), "Group job should have no pending jobs")
	})
}
