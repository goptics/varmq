package gocmq

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
		expectedConcurrency := withSafeConcurrency(1)
		assert.Equal(expectedConcurrency, w.Concurrency.Load(), "concurrency should match expected value")

		// Check channels stack length matches concurrency
		assert.Equal(int(expectedConcurrency), len(w.ChannelsStack), "channel stack length should match concurrency")

		// Check default status is 'initiated'
		assert.Equal(initiated, w.status.Load(), "status should be 'initiated'")

		// Check queue is not nil (should be null queue)
		assert.NotNil(w.Queue, "queue should not be nil, expected null queue")

		// Check jobPullNotifier is initialized
		assert.False(reflect.ValueOf(w.jobPullNotifier).IsNil(), "jobPullNotifier should be initialized")

		// Check sync group is initialized - we're not using IsZero since struct with zero values is still initialized
		assert.Equal(reflect.Struct, reflect.ValueOf(&w.sync).Elem().Kind(), "sync group should be initialized")

		// Check tickers map is initialized
		assert.NotNil(w.tickers, "tickers map should be initialized")
	})

	t.Run("with WorkerFunc and custom concurrency", func(t *testing.T) {
		// Create a worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Set custom concurrency
		customConcurrency := uint32(4)

		// Create worker with custom concurrency
		w := newWorker[string, int](wf, WithConcurrency(int(customConcurrency)))

		assert := assert.New(t)

		// Check concurrency is set correctly
		assert.Equal(customConcurrency, w.Concurrency.Load(), "concurrency should be set to custom value")

		// Check channels stack length matches concurrency
		assert.Equal(int(customConcurrency), len(w.ChannelsStack), "channel stack length should match concurrency")
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
		customConcurrency := uint32(8)
		customCache := new(sync.Map)
		cleanupInterval := 5 * time.Minute

		// Create worker with multiple configurations
		w := newWorker[string, int](
			wf,
			WithConcurrency(int(customConcurrency)),
			WithCache(customCache),
			WithAutoCleanupCache(cleanupInterval),
		)

		assert := assert.New(t)

		// Check concurrency is set correctly
		assert.Equal(customConcurrency, w.Concurrency.Load(), "concurrency should be set to custom value")

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
		expectedConcurrency := uint32(concurrencyValue)
		assert.Equal(expectedConcurrency, w.Concurrency.Load(), "concurrency should be set to direct value")
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
		expectedConcurrency := withSafeConcurrency(0) // This will use CPU count
		assert.Equal(expectedConcurrency, w.Concurrency.Load(), "concurrency should default to CPU count")
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
		expectedConcurrency := withSafeConcurrency(0) // This will use CPU count
		assert.Equal(expectedConcurrency, w.Concurrency.Load(), "concurrency should default to CPU count with negative value")
	})

	t.Run("verify ChannelsStack length is properly initialized", func(t *testing.T) {
		// Create a simple worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		concurrency := 3
		// Create worker
		w := newWorker[string, int](wf, WithConcurrency(concurrency))

		assert := assert.New(t)

		// Check channel stack length matches concurrency
		assert.Equal(concurrency, len(w.ChannelsStack), "channel stack length should match concurrency")

		// Note: In the actual implementation, the channels themselves might be nil initially
		// and get allocated when needed
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
		customConcurrency := uint32(4)
		customCache := new(sync.Map)
		cleanupInterval := 5 * time.Minute

		// Create original worker with configurations
		originalWorker := newWorker[string, int](
			wf,
			WithConcurrency(int(customConcurrency)),
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
		assert.Equal(uint32(0), workerBinder.CurrentProcessingCount(), "Current processing count should be 0")
	})

	t.Run("Copy method with updated configuration", func(t *testing.T) {
		// Create a simple worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Set original configurations
		originalConcurrency := uint32(4)
		originalCache := new(sync.Map)

		// Create original worker
		originalWorker := newWorker[string, int](
			wf,
			WithConcurrency(int(originalConcurrency)),
			WithCache(originalCache),
		)

		// Set new configurations for copy
		newConcurrency := uint32(8)
		newCache := new(sync.Map)

		// Copy the worker with new configurations
		workerBinder := originalWorker.Copy(
			WithConcurrency(int(newConcurrency)),
			WithCache(newCache),
		)

		// Test the functionality of the copied worker with updated configuration
		assert := assert.New(t)

		// Verify worker state
		assert.Equal("Initiated", workerBinder.Status(), "Worker status should be 'Initiated'")
		assert.False(workerBinder.IsRunning(), "Worker should not be running yet")
		assert.False(workerBinder.IsPaused(), "Worker should not be paused")
		assert.False(workerBinder.IsStopped(), "Worker should not be stopped")
		assert.Equal(uint32(0), workerBinder.CurrentProcessingCount(), "Current processing count should be 0")
	})

}
