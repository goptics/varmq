package varmq

// import (
// 	"reflect"
// 	"testing"
// 	"time"

// 	"github.com/stretchr/testify/assert"
// )

// func TestNewWorker(t *testing.T) {
// 	t.Run("with WorkerResultFunc and default configuration", func(t *testing.T) {
// 		// Create a simple worker function
// 		wf := func(data string) (int, error) {
// 			return len(data), nil
// 		}

// 		// Create worker with default config
// 		w := newWorker[string, int](wf)

// 		assert := assert.New(t)

// 		// Validate worker structure
// 		assert.NotNil(w, "worker should not be nil")

// 		// Check worker function
// 		assert.NotNil(w.workerFunc, "worker function should not be nil")

// 		// Check concurrency (should default to 1)
// 		assert.Equal(1, w.NumConcurrency(), "concurrency should match expected value")

// 		// Check default status is 'initiated'
// 		assert.Equal(initiated, w.status.Load(), "status should be 'initiated'")

// 		// Check queue is not nil (should be null queue)
// 		assert.NotNil(w.Queue, "queue should not be nil, expected null queue")

// 		// Check jobPullNotifier is initialized
// 		assert.False(reflect.ValueOf(w.jobPullNotifier).IsNil(), "jobPullNotifier should be initialized")

// 		// Check sync group is initialized - we're not using IsZero since struct with zero values is still initialized
// 		assert.Equal(reflect.Struct, reflect.ValueOf(&w.wg).Elem().Kind(), "wg should be initialized")

// 		// Check tickers map is initialized
// 		assert.NotNil(w.tickers, "tickers map should be initialized")
// 	})

// 	t.Run("with WorkerResultFunc and custom concurrency", func(t *testing.T) {
// 		// Create a worker function
// 		wf := func(data string) (int, error) {
// 			return len(data), nil
// 		}

// 		// Set custom concurrency
// 		customConcurrency := 4

// 		// Create worker with custom concurrency
// 		w := newWorker[string, int](wf, WithConcurrency(customConcurrency))

// 		assert := assert.New(t)

// 		// Check concurrency is set correctly
// 		assert.Equal(customConcurrency, w.NumConcurrency(), "concurrency should be set to custom value")
// 	})

// 	t.Run("with WorkerFunc and multiple configurations", func(t *testing.T) {
// 		// Create a worker function
// 		wf := func(data string) (int, error) {
// 			return len(data), nil
// 		}

// 		// Set custom configurations
// 		customConcurrency := 8
// 		cleanupInterval := 5 * time.Minute

// 		// Create worker with multiple configurations
// 		w := newWorker[string, int](
// 			wf,
// 			WithConcurrency(customConcurrency),
// 			WithAutoCleanupCache(cleanupInterval),
// 		)

// 		assert := assert.New(t)

// 		// Check concurrency is set correctly
// 		assert.Equal(customConcurrency, w.NumConcurrency(), "concurrency should be set to custom value")

// 		// Check cleanup interval is set correctly
// 		assert.Equal(cleanupInterval, w.CleanupCacheInterval, "cleanup interval should be set correctly")
// 	})

// 	t.Run("with WorkerErrFunc", func(t *testing.T) {
// 		// Create a worker error function
// 		wf := func(data string) error {
// 			return nil
// 		}

// 		// Create worker with error function
// 		w := newWorker[string, any](wf)

// 		assert := assert.New(t)

// 		// Validate worker structure
// 		assert.NotNil(w, "worker should not be nil")

// 		// Check worker function
// 		assert.NotNil(w.workerFunc, "worker function should not be nil")

// 		// Check function type matches WorkerErrFunc
// 		functionType := reflect.TypeOf(w.workerFunc)
// 		expectedFuncType := reflect.TypeOf(wf)
// 		assert.Equal(expectedFuncType, functionType, "function type should match WorkerErrFunc")
// 	})

// 	t.Run("with WorkerFunc", func(t *testing.T) {
// 		// Create a void worker function
// 		wf := func(data string) {
// 			// do nothing
// 		}

// 		// Create worker with void function
// 		w := newWorker[string, any](wf)

// 		assert := assert.New(t)

// 		// Validate worker structure
// 		assert.NotNil(w, "worker should not be nil")

// 		// Check worker function
// 		assert.NotNil(w.workerFunc, "worker function should not be nil")

// 		// Check function type matches WorkerFunc
// 		functionType := reflect.TypeOf(w.workerFunc)
// 		expectedFuncType := reflect.TypeOf(wf)
// 		assert.Equal(expectedFuncType, functionType, "function type should match WorkerFunc")
// 	})

// 	t.Run("with direct concurrency value instead of ConfigFunc", func(t *testing.T) {
// 		// Create a simple worker function
// 		wf := func(data string) (int, error) {
// 			return len(data), nil
// 		}

// 		// Set direct concurrency value
// 		concurrencyValue := 5

// 		// Create worker with direct concurrency value
// 		w := newWorker[string, int](wf, concurrencyValue)

// 		assert := assert.New(t)

// 		// Check concurrency is set correctly
// 		expectedConcurrency := concurrencyValue
// 		assert.Equal(expectedConcurrency, w.NumConcurrency(), "concurrency should be set to direct value")
// 	})

// 	t.Run("with custom job ID generator", func(t *testing.T) {
// 		// Create a simple worker function
// 		wf := func(data string) (int, error) {
// 			return len(data), nil
// 		}

// 		// Create custom job ID generator
// 		customIdGenerator := func() string {
// 			return "test-id"
// 		}

// 		// Create worker with custom job ID generator
// 		w := newWorker[string, int](wf, WithJobIdGenerator(customIdGenerator))

// 		assert := assert.New(t)

// 		// Check job ID generator is set correctly
// 		generatedId := w.JobIdGenerator()
// 		expectedId := "test-id"
// 		assert.Equal(expectedId, generatedId, "job ID generator should generate expected ID")
// 	})

// 	t.Run("with zero concurrency (should use CPU count)", func(t *testing.T) {
// 		// Create a simple worker function
// 		wf := func(data string) (int, error) {
// 			return len(data), nil
// 		}

// 		// Create worker with zero concurrency
// 		w := newWorker[string, int](wf, WithConcurrency(0))

// 		assert := assert.New(t)

// 		// Check concurrency equals CPU count
// 		assert.Equal(withSafeConcurrency(0), w.concurrency.Load(), "concurrency should default to CPU count")
// 	})

// 	t.Run("with negative concurrency (should use CPU count)", func(t *testing.T) {
// 		// Create a simple worker function
// 		wf := func(data string) (int, error) {
// 			return len(data), nil
// 		}

// 		// Create worker with negative concurrency
// 		w := newWorker[string, int](wf, WithConcurrency(-5))

// 		assert := assert.New(t)

// 		// Check concurrency equals CPU count
// 		assert.Equal(withSafeConcurrency(0), w.concurrency.Load(), "concurrency should default to CPU count with negative value")
// 	})

// 	t.Run("verify initial status is 'initiated'", func(t *testing.T) {
// 		// Create a simple worker function
// 		wf := func(data string) (int, error) {
// 			return len(data), nil
// 		}

// 		// Create worker
// 		w := newWorker[string, int](wf)

// 		assert := assert.New(t)

// 		// Check status is 'initiated'
// 		assert.Equal(initiated, w.status.Load(), "Worker status should be 'initiated'")

// 		// Check the string representation of status
// 		assert.Equal("Initiated", w.Status(), "Worker status string should be 'Initiated'")
// 	})
// }

// func TestCurrentConcurrency(t *testing.T) {
// 	// Create a simple worker function that we'll use across tests
// 	wf := func(data string) (int, error) {
// 		return len(data), nil
// 	}

// 	t.Run("initial concurrency value", func(t *testing.T) {
// 		// Test with explicit concurrency value
// 		concurrencyValue := 4
// 		w := newWorker[string, int](wf, WithConcurrency(concurrencyValue))

// 		// Verify initial concurrency value
// 		assert.Equal(t, concurrencyValue, w.NumConcurrency(), "CurrentConcurrency should return the initial concurrency value")
// 	})

// 	t.Run("default concurrency value", func(t *testing.T) {
// 		// Create worker with default concurrency
// 		w := newWorker[string, int](wf)

// 		// Default concurrency should match the safe concurrency for 1
// 		expectedConcurrency := int(withSafeConcurrency(1))
// 		assert.Equal(t, expectedConcurrency, w.NumConcurrency(), "CurrentConcurrency should return the default concurrency value")
// 	})

// 	t.Run("concurrency after tuning", func(t *testing.T) {
// 		// Create worker with initial concurrency
// 		initialConcurrency := 2
// 		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

// 		// Start the worker to allow tuning
// 		err := w.start()
// 		assert.NoError(t, err, "Worker should start without error")
// 		defer w.Stop() // Clean up

// 		// Verify initial concurrency
// 		assert.Equal(t, initialConcurrency, w.NumConcurrency(), "CurrentConcurrency should match initial value")

// 		// Tune concurrency
// 		newConcurrency := 5
// 		err = w.TunePool(newConcurrency)
// 		assert.NoError(t, err, "TunePool should not return error")

// 		// Verify concurrency was updated
// 		assert.Equal(t, newConcurrency, w.NumConcurrency(), "CurrentConcurrency should return updated value after tuning")
// 	})
// }

// func TestTunePool(t *testing.T) {
// 	// Create a simple worker function that we'll use across tests
// 	wf := func(data string) (int, error) {
// 		return len(data), nil
// 	}

// 	t.Run("worker not running error", func(t *testing.T) {
// 		// Create worker but don't start it
// 		w := newWorker[string, int](wf, WithConcurrency(2))

// 		// Try to tune concurrency on non-running worker
// 		err := w.TunePool(4)

// 		// Verify error is returned
// 		assert.Error(t, err, "TunePool should return error when worker is not running")
// 		assert.Equal(t, errNotRunningWorker, err, "Should return specific 'worker not running' error")
// 	})

// 	t.Run("increase concurrency", func(t *testing.T) {
// 		// Create worker with initial concurrency of 2
// 		initialConcurrency := 2
// 		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

// 		// Initialize worker
// 		err := w.start()
// 		assert.NoError(t, err, "Worker should start without error")
// 		defer w.Stop() // Clean up

// 		// Confirm initial state
// 		assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")

// 		// Tune concurrency up to 5
// 		newConcurrency := 5
// 		err = w.TunePool(newConcurrency)
// 		assert.NoError(t, err, "TunePool should not return error on running worker")

// 		// Verify updated concurrency
// 		assert.Equal(t, newConcurrency, w.NumConcurrency(), "Concurrency should be updated to new value")

// 		// Allow time for channels to be created and added to stack
// 		time.Sleep(100 * time.Millisecond)

// 		// Check that new worker goroutines were started (channel stack will have more capacity)
// 		// We can't directly check stack size as channels are consumed in testing
// 		assert.Equal(t, newConcurrency, w.NumConcurrency(), "Stack should reflect the new concurrency")
// 	})

// 	t.Run("decrease concurrency", func(t *testing.T) {
// 		// Create worker with initial concurrency of 5
// 		initialConcurrency := 5
// 		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

// 		// Initialize worker
// 		err := w.start()
// 		assert.NoError(t, err, "Worker should start without error")
// 		defer w.Stop() // Clean up

// 		// Confirm initial state
// 		assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")

// 		// Tune concurrency down to 2
// 		newConcurrency := 2
// 		err = w.TunePool(newConcurrency)
// 		assert.NoError(t, err, "TunePool should not return error on running worker")

// 		// Verify updated concurrency
// 		assert.Equal(t, newConcurrency, w.NumConcurrency(), "Concurrency should be updated to new lower value")

// 		// Allow time for channels to be closed
// 		time.Sleep(100 * time.Millisecond)

// 		// Verify the worker is still operational
// 		assert.True(t, w.IsRunning(), "Worker should still be running after decreasing concurrency")
// 	})

// 	t.Run("set concurrency to zero", func(t *testing.T) {
// 		// Create worker with initial concurrency of 3
// 		initialConcurrency := 3
// 		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

// 		// Initialize worker
// 		err := w.start()
// 		assert.NoError(t, err, "Worker should start without error")
// 		defer w.Stop() // Clean up

// 		// Try to tune concurrency to 0 (should result in safe minimum concurrency)
// 		err = w.TunePool(0)
// 		assert.NoError(t, err, "TunePool should not return error on running worker")

// 		// Verify minimum safe concurrency is used instead of 0
// 		assert.Equal(t, withSafeConcurrency(0), w.concurrency.Load(), "Should use minimum safe concurrency when 0 is provided")
// 	})

// 	t.Run("set concurrency to negative value", func(t *testing.T) {
// 		// Create worker with initial concurrency of 3
// 		initialConcurrency := 3
// 		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

// 		// Initialize worker
// 		err := w.start()
// 		assert.NoError(t, err, "Worker should start without error")
// 		defer w.Stop() // Clean up

// 		// Try to tune concurrency to -5 (should result in safe minimum concurrency)
// 		err = w.TunePool(-5)
// 		assert.NoError(t, err, "TunePool should not return error on running worker")

// 		// Verify minimum safe concurrency is used instead of negative value
// 		assert.Equal(t, withSafeConcurrency(-5), w.concurrency.Load(), "Should use minimum safe concurrency when negative value is provided")
// 	})

// 	t.Run("same concurrency value", func(t *testing.T) {
// 		// Create worker with initial concurrency of 4
// 		initialConcurrency := 4
// 		w := newWorker[string, int](wf, WithConcurrency(initialConcurrency))

// 		// Initialize worker
// 		err := w.start()
// 		assert.NoError(t, err, "Worker should start without error")
// 		defer w.Stop() // Clean up

// 		// Confirm initial state
// 		assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")

// 		// "Tune" to the same concurrency value
// 		err = w.TunePool(initialConcurrency)
// 		assert.ErrorIs(t, err, errSameConcurrency, "TunePool should return error when concurrency is the same")

// 		// Verify concurrency remains unchanged
// 		assert.Equal(t, initialConcurrency, w.NumConcurrency(), "Concurrency should remain unchanged when set to same value")
// 	})
// }
