package varmq

// import (
// 	"strconv"
// 	"testing"

// 	"github.com/stretchr/testify/assert"

// 	"github.com/goptics/varmq/internal/collections"
// )

// func initQueue() (*queue[string], *worker[string, int], *collections.Queue[any]) {
// 	// Create a worker with a simple process function that doubles an integer
// 	var workerFunc WorkerResultFunc[string, int]

// 	workerFunc = func(data string) (int, error) {
// 		// Simple processor that converts string to int and doubles it
// 		val, err := strconv.Atoi(data)
// 		if err != nil {
// 			return 0, err
// 		}
// 		return val * 2, nil
// 	}

// 	// Create an internal queue from collections and bind the worker to it
// 	internalQueue := collections.NewQueue[any]()
// 	worker := newWorker[string, int](workerFunc)
// 	queue := newQueue(worker, internalQueue)

// 	return queue, worker, internalQueue
// }

// // TestQueueAdd tests the Add method of the queue
// func TestQueue(t *testing.T) {
// 	t.Run("Start worker", func(t *testing.T) {
// 		_, worker, _ := initQueue()
// 		err := worker.start()
// 		assert.Nil(t, err, "Worker should start successfully")
// 	})

// 	t.Run("Adding job to queue", func(t *testing.T) {
// 		queue, _, internalQueue := initQueue()

// 		// Test adding a job
// 		job, ok := queue.Add("42")
// 		assert.True(t, ok, "Job should be added successfully")
// 		assert.NotNil(t, job, "Job should not be nil")
// 		assert.Equal(t, 1, queue.NumPending(), "Queue should have one pending job")
// 		assert.Equal(t, 1, internalQueue.Len(), "Internal Queue should have one item")
// 	})

// 	t.Run("Processing job", func(t *testing.T) {
// 		queue, worker, internalQueue := initQueue()

// 		err := worker.start()
// 		assert.Nil(t, err, "Worker should start successfully")

// 		job, _ := queue.Add("42")
// 		job.Result()
// 		assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
// 		assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
// 	})

// 	t.Run("Adding multiple jobs to queue", func(t *testing.T) {
// 		jobs := []Item[string]{
// 			{Value: "1", ID: "job1"},
// 			{Value: "2", ID: "job2"},
// 			{Value: "3", ID: "job3"},
// 			{Value: "4", ID: "job4"},
// 			{Value: "5", ID: "job5"},
// 		}

// 		queue, worker, internalQueue := initQueue()

// 		groupJob := queue.AddAll(jobs)
// 		pending := queue.NumPending()
// 		assert.Equal(t, 5, pending, "Queue should have five pending jobs")
// 		assert.Equal(t, pending, internalQueue.Len(), "Internal Queue should have five items")
// 		assert.Equal(t, pending, groupJob.Len(), "Group job should have five items")

// 		err := worker.start()
// 		assert.Nil(t, err, "Worker should start successfully")

// 		results, _ := groupJob.Results()
// 		_, err = groupJob.Results()
// 		assert.NotNil(t, err, "Results channel should not be accessible more than once")

// 		// Collect all results
// 		var got []int
// 		for r := range results {
// 			got = append(got, r.Data)
// 		}
// 		// Expect one result per job
// 		assert.Len(t, got, len(jobs))
// 		// Build expected set of doubled values
// 		expected := make(map[int]struct{}, len(jobs))
// 		for _, j := range jobs {
// 			v, _ := strconv.Atoi(j.Value)
// 			expected[v*2] = struct{}{}
// 		}
// 		// Verify each output is one of the expected doubles
// 		for _, val := range got {
// 			_, ok := expected[val]
// 			assert.True(t, ok, "unexpected result value: %d", val)
// 		}

// 		assert.Equal(t, 0, queue.NumPending(), "Queue should have no pending jobs")
// 		assert.Equal(t, 0, internalQueue.Len(), "Internal Queue should be empty")
// 		assert.Equal(t, 0, groupJob.Len(), "Group job should have no pending jobs")
// 	})
// }
