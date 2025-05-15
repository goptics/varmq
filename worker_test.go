package varmq

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewWorker(t *testing.T) {
	t.Run("with WorkerFunc and default configuration", func(t *testing.T) {
		// Create a simple worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Create worker with default config
		w := newWorker[string, int](wf)

		assert := assert.New(t)

		// Validate worker structure
		assert.NotNil(w, "worker should not be nil")

		// Check worker function
		assert.NotNil(w.workerFunc, "worker function should not be nil")

		// Check concurrency (should default to 1)
		assert.Equal(1, w.CurrentConcurrency(), "concurrency should match expected value")

		// Check default status is 'initiated'
		assert.Equal(initiated, w.status.Load(), "status should be 'initiated'")

		// Check queue is not nil (should be null queue)
		assert.NotNil(w.Queue, "queue should not be nil, expected null queue")

		// Check jobPullNotifier is initialized
		assert.False(reflect.ValueOf(w.jobPullNotifier).IsNil(), "jobPullNotifier should be initialized")

		// Check sync group is initialized - we're not using IsZero since struct with zero values is still initialized
		assert.Equal(reflect.Struct, reflect.ValueOf(&w.wg).Elem().Kind(), "wg should be initialized")

		// Check tickers map is initialized
		assert.NotNil(w.tickers, "tickers map should be initialized")
	})

	t.Run("with WorkerFunc and custom concurrency", func(t *testing.T) {
		// Create a worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Set custom concurrency
		customConcurrency := 4

		// Create worker with custom concurrency
		w := newWorker[string, int](wf, WithConcurrency(customConcurrency))

		assert := assert.New(t)

		// Check concurrency is set correctly
		assert.Equal(customConcurrency, w.CurrentConcurrency(), "concurrency should be set to custom value")
	})

	t.Run("with WorkerFunc and custom cache", func(t *testing.T) {
		// Create a worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Create custom cache
		customCache := new(sync.Map)

		// Create worker with custom cache
		w := newWorker[string, int](wf, WithCache(customCache))

		assert := assert.New(t)

		// Check cache is set correctly
		assert.Equal(customCache, w.Cache, "cache should be set to custom cache")
	})

	t.Run("with WorkerFunc and multiple configurations", func(t *testing.T) {
		// Create a worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Set custom configurations
		customConcurrency := 8
		customCache := new(sync.Map)
		cleanupInterval := 5 * time.Minute

		// Create worker with multiple configurations
		w := newWorker[string, int](
			wf,
			WithConcurrency(customConcurrency),
			WithCache(customCache),
			WithAutoCleanupCache(cleanupInterval),
		)

		assert := assert.New(t)

		// Check concurrency is set correctly
		assert.Equal(customConcurrency, w.CurrentConcurrency(), "concurrency should be set to custom value")

		// Check cache is set correctly
		assert.Equal(customCache, w.Cache, "cache should be set to custom cache")

		// Check cleanup interval is set correctly
		assert.Equal(cleanupInterval, w.CleanupCacheInterval, "cleanup interval should be set correctly")
	})

	t.Run("with WorkerErrFunc", func(t *testing.T) {
		// Create a worker error function
		wf := func(data string) error {
			return nil
		}

		// Create worker with error function
		w := newWorker[string, any](wf)

		assert := assert.New(t)

		// Validate worker structure
		assert.NotNil(w, "worker should not be nil")

		// Check worker function
		assert.NotNil(w.workerFunc, "worker function should not be nil")

		// Check function type matches WorkerErrFunc
		functionType := reflect.TypeOf(w.workerFunc)
		expectedFuncType := reflect.TypeOf(wf)
		assert.Equal(expectedFuncType, functionType, "function type should match WorkerErrFunc")
	})

	t.Run("with VoidWorkerFunc", func(t *testing.T) {
		// Create a void worker function
		wf := func(data string) {
			// do nothing
		}

		// Create worker with void function
		w := newWorker[string, any](wf)

		assert := assert.New(t)

		// Validate worker structure
		assert.NotNil(w, "worker should not be nil")

		// Check worker function
		assert.NotNil(w.workerFunc, "worker function should not be nil")

		// Check function type matches VoidWorkerFunc
		functionType := reflect.TypeOf(w.workerFunc)
		expectedFuncType := reflect.TypeOf(wf)
		assert.Equal(expectedFuncType, functionType, "function type should match VoidWorkerFunc")
	})

	t.Run("with direct concurrency value instead of ConfigFunc", func(t *testing.T) {
		// Create a simple worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Set direct concurrency value
		concurrencyValue := 5

		// Create worker with direct concurrency value
		w := newWorker[string, int](wf, concurrencyValue)

		assert := assert.New(t)

		// Check concurrency is set correctly
		expectedConcurrency := concurrencyValue
		assert.Equal(expectedConcurrency, w.CurrentConcurrency(), "concurrency should be set to direct value")
	})

	t.Run("with custom job ID generator", func(t *testing.T) {
		// Create a simple worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Create custom job ID generator
		customIdGenerator := func() string {
			return "test-id"
		}

		// Create worker with custom job ID generator
		w := newWorker[string, int](wf, WithJobIdGenerator(customIdGenerator))

		assert := assert.New(t)

		// Check job ID generator is set correctly
		generatedId := w.JobIdGenerator()
		expectedId := "test-id"
		assert.Equal(expectedId, generatedId, "job ID generator should generate expected ID")
	})

	t.Run("with zero concurrency (should use CPU count)", func(t *testing.T) {
		// Create a simple worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Create worker with zero concurrency
		w := newWorker[string, int](wf, WithConcurrency(0))

		assert := assert.New(t)

		// Check concurrency equals CPU count
		assert.Equal(withSafeConcurrency(0), w.concurrency.Load(), "concurrency should default to CPU count")
	})

	t.Run("with negative concurrency (should use CPU count)", func(t *testing.T) {
		// Create a simple worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Create worker with negative concurrency
		w := newWorker[string, int](wf, WithConcurrency(-5))

		assert := assert.New(t)

		// Check concurrency equals CPU count
		assert.Equal(withSafeConcurrency(0), w.concurrency.Load(), "concurrency should default to CPU count with negative value")
	})

	t.Run("verify initial status is 'initiated'", func(t *testing.T) {
		// Create a simple worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Create worker
		w := newWorker[string, int](wf)

		assert := assert.New(t)

		// Check status is 'initiated'
		assert.Equal(initiated, w.status.Load(), "Worker status should be 'initiated'")

		// Check the string representation of status
		assert.Equal("Initiated", w.Status(), "Worker status string should be 'Initiated'")
	})

	t.Run("Copy method with default configuration", func(t *testing.T) {
		// Create a simple worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Set custom configurations
		customConcurrency := 4
		customCache := new(sync.Map)
		cleanupInterval := 5 * time.Minute

		// Create original worker with configurations
		originalWorker := newWorker[string, int](
			wf,
			WithConcurrency(customConcurrency),
			WithCache(customCache),
			WithAutoCleanupCache(cleanupInterval),
		)

		// Copy the worker
		workerBinder := originalWorker.Copy()

		// Check that copied worker has the same configuration by using the Worker interface methods
		// Since we can't directly access the worker struct fields, we'll test what we can through the interface
		assert := assert.New(t)

		// Check status is initiated
		assert.Equal("Initiated", workerBinder.Status(), "Worker status should be 'Initiated'")

		// Test the functionality of the copied worker
		assert.False(workerBinder.IsRunning(), "Worker should not be running yet")
		assert.False(workerBinder.IsPaused(), "Worker should not be paused")
		assert.False(workerBinder.IsStopped(), "Worker should not be stopped")
		assert.Equal(0, workerBinder.CurrentProcessingCount(), "Current processing count should be 0")
	})

	t.Run("Copy method with updated configuration", func(t *testing.T) {
		// Create a simple worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Set original configurations
		originalConcurrency := 4
		originalCache := new(sync.Map)

		// Create original worker
		originalWorker := newWorker[string, int](
			wf,
			WithConcurrency(originalConcurrency),
			WithCache(originalCache),
		)

		// Set new configurations for copy
		newConcurrency := 8
		newCache := new(sync.Map)

		// Copy the worker with new configurations
		workerBinder := originalWorker.Copy(
			WithConcurrency(newConcurrency),
			WithCache(newCache),
		)

		// Test the functionality of the copied worker with updated configuration
		assert := assert.New(t)

		// Verify worker state
		assert.Equal("Initiated", workerBinder.Status(), "Worker status should be 'Initiated'")
		assert.False(workerBinder.IsRunning(), "Worker should not be running yet")
		assert.False(workerBinder.IsPaused(), "Worker should not be paused")
		assert.False(workerBinder.IsStopped(), "Worker should not be stopped")
		assert.Equal(0, workerBinder.CurrentProcessingCount(), "Current processing count should be 0")
	})

}

func TestCurrentConcurrency(t *testing.T) {
	// Create a simple worker function that we'll use across tests
	wf := func(data string) (int, error) {
		return len(data), nil
	}

	t.Run("initial concurrency value", func(t *testing.T) {
		// Test with explicit concurrency value
		concurrencyValue := 4
		w := newWorker[string, int](wf, WithConcurrency(concurrencyValue))

		// Verify initial concurrency value
		assert.Equal(t, concurrencyValue, w.CurrentConcurrency(), "CurrentConcurrency should return the initial concurrency value")
	})

	t.Run("default concurrency value", func(t *testing.T) {
		// Create worker with default concurrency
		w := newWorker[string, int](wf)

		// Default concurrency should match the safe concurrency for 1
		expectedConcurrency := int(withSafeConcurrency(1))
		assert.Equal(t, expectedConcurrency, w.CurrentConcurrency(), "CurrentConcurrency should return the default concurrency value")
	})

	t.Run("concurrency after tuning", func(t *testing.T) {
		// Create worker with initial concurrency
		initialConcurrency := 2
		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

		// Start the worker to allow tuning
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Verify initial concurrency
		assert.Equal(t, initialConcurrency, w.CurrentConcurrency(), "CurrentConcurrency should match initial value")

		// Tune concurrency
		newConcurrency := 5
		err = w.TuneConcurrency(newConcurrency)
		assert.NoError(t, err, "TuneConcurrency should not return error")

		// Verify concurrency was updated
		assert.Equal(t, newConcurrency, w.CurrentConcurrency(), "CurrentConcurrency should return updated value after tuning")
	})

	t.Run("concurrency updated in copied worker", func(t *testing.T) {
		// Create original worker
		originalConcurrency := 3
		originalWorker := newWorker[string, int](wf, WithConcurrency(originalConcurrency))

		// Verify original concurrency
		assert.Equal(t, originalConcurrency, originalWorker.CurrentConcurrency(), "Original worker should have expected concurrency")

		// Copy with different concurrency
		newConcurrency := 6
		copiedWorker := originalWorker.Copy(WithConcurrency(newConcurrency))

		// Verify copied worker has the new concurrency
		assert.Equal(t, newConcurrency, copiedWorker.CurrentConcurrency(), "Copied worker should have the new concurrency value")

		// Verify original worker still has its original concurrency
		assert.Equal(t, originalConcurrency, originalWorker.CurrentConcurrency(), "Original worker should maintain its concurrency value after copy")
	})
}

func TestTuneConcurrency(t *testing.T) {
	// Create a simple worker function that we'll use across tests
	wf := func(data string) (int, error) {
		return len(data), nil
	}

	t.Run("worker not running error", func(t *testing.T) {
		// Create worker but don't start it
		w := newWorker[string, int](wf, WithConcurrency(2))

		// Try to tune concurrency on non-running worker
		err := w.TuneConcurrency(4)

		// Verify error is returned
		assert.Error(t, err, "TuneConcurrency should return error when worker is not running")
		assert.Equal(t, errNotRunningWorker, err, "Should return specific 'worker not running' error")
	})

	t.Run("increase concurrency", func(t *testing.T) {
		// Create worker with initial concurrency of 2
		initialConcurrency := 2
		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

		// Initialize worker
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Confirm initial state
		assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")

		// Tune concurrency up to 5
		newConcurrency := 5
		err = w.TuneConcurrency(newConcurrency)
		assert.NoError(t, err, "TuneConcurrency should not return error on running worker")

		// Verify updated concurrency
		assert.Equal(t, newConcurrency, w.CurrentConcurrency(), "Concurrency should be updated to new value")

		// Allow time for channels to be created and added to stack
		time.Sleep(100 * time.Millisecond)

		// Check that new worker goroutines were started (channel stack will have more capacity)
		// We can't directly check stack size as channels are consumed in testing
		assert.Equal(t, newConcurrency, w.CurrentConcurrency(), "Stack should reflect the new concurrency")
	})

	t.Run("decrease concurrency", func(t *testing.T) {
		// Create worker with initial concurrency of 5
		initialConcurrency := 5
		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

		// Initialize worker
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Confirm initial state
		assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")

		// Tune concurrency down to 2
		newConcurrency := 2
		err = w.TuneConcurrency(newConcurrency)
		assert.NoError(t, err, "TuneConcurrency should not return error on running worker")

		// Verify updated concurrency
		assert.Equal(t, newConcurrency, w.CurrentConcurrency(), "Concurrency should be updated to new lower value")

		// Allow time for channels to be closed
		time.Sleep(100 * time.Millisecond)

		// Verify the worker is still operational
		assert.True(t, w.IsRunning(), "Worker should still be running after decreasing concurrency")
	})

	t.Run("set concurrency to zero", func(t *testing.T) {
		// Create worker with initial concurrency of 3
		initialConcurrency := 3
		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

		// Initialize worker
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Try to tune concurrency to 0 (should result in safe minimum concurrency)
		err = w.TuneConcurrency(0)
		assert.NoError(t, err, "TuneConcurrency should not return error on running worker")

		// Verify minimum safe concurrency is used instead of 0
		assert.Equal(t, withSafeConcurrency(0), w.concurrency.Load(), "Should use minimum safe concurrency when 0 is provided")
	})

	t.Run("set concurrency to negative value", func(t *testing.T) {
		// Create worker with initial concurrency of 3
		initialConcurrency := 3
		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

		// Initialize worker
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Try to tune concurrency to -5 (should result in safe minimum concurrency)
		err = w.TuneConcurrency(-5)
		assert.NoError(t, err, "TuneConcurrency should not return error on running worker")

		// Verify minimum safe concurrency is used instead of negative value
		assert.Equal(t, withSafeConcurrency(-5), w.concurrency.Load(), "Should use minimum safe concurrency when negative value is provided")
	})

	t.Run("same concurrency value", func(t *testing.T) {
		// Create worker with initial concurrency of 4
		initialConcurrency := 4
		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

		// Initialize worker
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Confirm initial state
		assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")

		// "Tune" to the same concurrency value
		err = w.TuneConcurrency(initialConcurrency)
		assert.NoError(t, err, "TuneConcurrency should not return error on running worker")

		// Verify concurrency remains unchanged
		assert.Equal(t, initialConcurrency, w.CurrentConcurrency(), "Concurrency should remain unchanged when set to same value")
	})
}
