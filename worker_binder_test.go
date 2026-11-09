package varmq

import (
	"errors"
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/goptics/varmq/mocks"
)

// TestWorkerBinders tests the worker binder implementations
func TestWorkerBinders(t *testing.T) {
	// Test standard worker binder
	t.Run("BindQueue", func(t *testing.T) {
		// Create a worker
		w := newWorker(func(j iJob[string]) {})

		// Create a worker binder
		binder := newQueues(w)

		// Test BindQueue
		queue := binder.BindQueue()
		assert.NotNil(t, queue, "Queue should not be nil")

		// Verify queue is not nil
		assert.NotNil(t, queue, "Queue should not be nil")
		assert.True(t, w.IsIdle(), "Worker should be idle after binding queue")

		// Clean up
		w.Stop()
	})

	t.Run("BindPriorityQueue", func(t *testing.T) {
		// Create a worker
		w := newWorker(func(j iJob[string]) {})

		// Create a worker binder
		binder := newQueues(w)

		// Test BindPriorityQueue
		pQueue := binder.BindPriorityQueue()
		assert.NotNil(t, pQueue, "PriorityQueue should not be nil")

		// Verify priority queue is not nil
		assert.NotNil(t, pQueue, "PriorityQueue should not be nil")
		assert.True(t, w.IsIdle(), "Worker should be idle after binding priority queue")

		// Clean up
		w.Stop()
	})

	t.Run("ResultHasQueueMethods", func(t *testing.T) {
		// Create a result worker
		w := newResultWorker(func(j iResultJob[string, int]) {
			j.sendResult(len(j.Data()))
		})
		defer w.Stop()

		// Create a result worker binder
		binder := newResultQueues(w)

		// Use reflection to check if the methods exist
		binderType := reflect.TypeOf(binder)

		// Check for queue methods
		_, exists := binderType.MethodByName("BindQueue")
		assert.True(t, exists, "ResultWorkerBinder should have BindQueue method")

		_, exists = binderType.MethodByName("WithQueue")
		assert.True(t, exists, "ResultWorkerBinder should have WithQueue method")

		// Check for priority queue methods
		_, exists = binderType.MethodByName("BindPriorityQueue")
		assert.True(t, exists, "ResultWorkerBinder should have BindPriorityQueue method")

		_, exists = binderType.MethodByName("WithPriorityQueue")
		assert.True(t, exists, "ResultWorkerBinder should have WithPriorityQueue method")
	})

	t.Run("ErrHasQueueMethods", func(t *testing.T) {
		// Create an error worker
		w := newErrWorker(func(j iErrorJob[string]) {
			j.sendError(nil)
		})
		defer w.Stop()

		// Create an error worker binder
		binder := newErrQueues(w)

		// Use reflection to check if the methods exist
		binderType := reflect.TypeOf(binder)

		// Check for queue methods
		_, exists := binderType.MethodByName("BindQueue")
		assert.True(t, exists, "ErrWorkerBinder should have BindQueue method")

		_, exists = binderType.MethodByName("WithQueue")
		assert.True(t, exists, "ErrWorkerBinder should have WithQueue method")

		// Check for priority queue methods
		_, exists = binderType.MethodByName("BindPriorityQueue")
		assert.True(t, exists, "ErrWorkerBinder should have BindPriorityQueue method")

		_, exists = binderType.MethodByName("WithPriorityQueue")
		assert.True(t, exists, "ErrWorkerBinder should have WithPriorityQueue method")
	})

	// Test result worker binder
	t.Run("ResultBindQueue", func(t *testing.T) {
		// Create a result worker
		w := newResultWorker(func(j iResultJob[string, int]) {
			j.sendResult(len(j.Data()))
		})

		// Create a result worker binder
		binder := newResultQueues(w)

		// Test BindQueue
		queue := binder.BindQueue()
		assert.NotNil(t, queue, "ResultQueue should not be nil")

		// Verify queue is not nil
		assert.NotNil(t, queue, "ResultQueue should not be nil")
		assert.True(t, w.IsIdle(), "Worker should be idle after binding queue")

		// Clean up
		w.Stop()
	})

	t.Run("ResultBindPriorityQueue", func(t *testing.T) {
		// Create a result worker
		w := newResultWorker(func(j iResultJob[string, int]) {
			j.sendResult(len(j.Data()))
		})

		// Create a result worker binder
		binder := newResultQueues(w)

		// Test BindPriorityQueue
		pQueue := binder.BindPriorityQueue()
		assert.NotNil(t, pQueue, "ResultPriorityQueue should not be nil")

		// Verify priority queue is not nil
		assert.NotNil(t, pQueue, "ResultPriorityQueue should not be nil")
		assert.True(t, w.IsIdle(), "Worker should be idle after binding priority queue")

		// Clean up
		w.Stop()
	})

	// Test error worker binder
	t.Run("ErrBindQueue", func(t *testing.T) {
		// Create an error worker
		w := newErrWorker(func(j iErrorJob[string]) {
			j.sendError(nil)
		})

		// Create an error worker binder
		binder := newErrQueues(w)

		// Test BindQueue
		queue := binder.BindQueue()
		assert.NotNil(t, queue, "ErrQueue should not be nil")

		// Verify queue is not nil
		assert.NotNil(t, queue, "ErrQueue should not be nil")
		assert.True(t, w.IsIdle(), "Worker should be idle after binding queue")

		// Clean up
		w.Stop()
	})

	t.Run("ErrBindPriorityQueue", func(t *testing.T) {
		// Create an error worker
		w := newErrWorker(func(j iErrorJob[string]) {
			j.sendError(nil)
		})

		// Create an error worker binder
		binder := newErrQueues(w)

		// Test BindPriorityQueue
		pQueue := binder.BindPriorityQueue()
		assert.NotNil(t, pQueue, "ErrPriorityQueue should not be nil")

		// Verify priority queue is not nil and is of the expected type ErrPriorityQueue[string]
		assert.NotNil(t, pQueue, "ErrPriorityQueue should not be nil")
		assert.True(t, w.IsIdle(), "Worker should be idle after binding priority queue")

		// Clean up
		w.Stop()
	})
}

func TestResultWorkerPersistent(t *testing.T) {
	w := NewResultWorker(func(j Job[string]) (int, error) {
		if j.Data() == "bad" {
			return 0, errors.New("bad data")
		}
		return len(j.Data()), nil
	})
	defer w.Stop()

	store := mocks.NewMockPersistentQueue()
	q := w.WithQueue(store)
	for _, d := range []string{"a", "bad", "c"} {
		_, ok := q.Add(d)
		assert.True(t, ok)
	}

	assert.Eventually(t, func() bool { return len(store.Acked())+len(store.Rejected()) == 3 }, time.Second, 5*time.Millisecond)
	assert.Len(t, store.Acked(), 2)
	assert.Equal(t, []string{"bad data"}, store.Rejected())
}

func TestErrWorkerPersistent(t *testing.T) {
	w := NewErrWorker(func(j Job[string]) error {
		if j.Data() == "bad" {
			return errors.New("bad data")
		}
		return nil
	})
	defer w.Stop()

	store := mocks.NewMockPersistentPriorityQueue()
	q := w.WithPriorityQueue(store)
	for i, d := range []string{"a", "bad", "c"} {
		_, ok := q.Add(d, i)
		assert.True(t, ok)
	}

	assert.Eventually(t, func() bool { return len(store.Acked())+len(store.Rejected()) == 3 }, time.Second, 5*time.Millisecond)
	assert.Len(t, store.Acked(), 2)
	assert.Equal(t, []string{"bad data"}, store.Rejected())
}

func TestResultWorkerDistributed(t *testing.T) {
	w := NewResultWorker(func(j Job[string]) (int, error) {
		if j.Data() == "bad" {
			return 0, errors.New("bad data")
		}
		return len(j.Data()), nil
	})
	defer w.Stop()

	store := mocks.NewMockDistributedQueue()
	q := w.WithQueue(store)
	for _, d := range []string{"a", "bad", "c"} {
		_, ok := q.Add(d)
		assert.True(t, ok)
	}

	assert.Eventually(t, func() bool { return len(store.Acked())+len(store.Rejected()) == 3 }, time.Second, 5*time.Millisecond)
	assert.Len(t, store.Acked(), 2)
	assert.Equal(t, []string{"bad data"}, store.Rejected())
}

func TestParseToJobKeepsWorkflowTagInResultJob(t *testing.T) {
	src := newJob("x", jobConfigs{Id: "wf:w1:a:undo"})
	src.setTask(taskTag{wf: "w1", task: "a", undo: true})
	b, err := src.Json()
	require.NoError(t, err)

	j, err := parseToJob(b, newResultJob[string, int])
	require.NoError(t, err)

	tag := j.taskTag()
	require.NotNil(t, tag)
	assert.Equal(t, "w1", tag.wf)
	assert.Equal(t, "a", tag.task)
	assert.True(t, tag.undo)
	assert.Equal(t, "x", j.Data())
	assert.Equal(t, "wf:w1:a:undo", j.ID())
}
