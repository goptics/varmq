package varmq

import (
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goptics/varmq/internal/queues"
	"github.com/stretchr/testify/assert"
)

func TestNewWorker(t *testing.T) {
	t.Run("with WorkerResultFunc and default configuration", func(t *testing.T) {
		// Create a simple worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Create worker with default config
		w := newResultWorker(wf)

		assert := assert.New(t)

		// Validate worker structure
		assert.NotNil(w, "worker should not be nil")

		// Check worker function
		assert.NotNil(w.workerFunc, "worker function should not be nil")

		// Check concurrency (should default to 1)
		assert.Equal(1, w.NumConcurrency(), "concurrency should match expected value")

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

	t.Run("with WorkerResultFunc and custom concurrency", func(t *testing.T) {
		// Create a worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Set custom concurrency
		customConcurrency := 4

		// Create worker with custom concurrency
		w := newResultWorker(wf, WithConcurrency(customConcurrency))

		assert := assert.New(t)

		// Check concurrency is set correctly
		assert.Equal(customConcurrency, w.NumConcurrency(), "concurrency should be set to custom value")
	})

	t.Run("with WorkerResultFunc and multiple configurations", func(t *testing.T) {
		// Create a worker function
		wf := func(data string) (int, error) {
			return len(data), nil
		}

		// Set custom configurations
		customConcurrency := 8
		cleanupInterval := 5 * time.Minute

		// Create worker with multiple configurations
		w := newResultWorker(
			wf,
			WithConcurrency(customConcurrency),
			WithAutoCleanupCache(cleanupInterval),
		)

		assert := assert.New(t)

		// Check concurrency is set correctly
		assert.Equal(customConcurrency, w.NumConcurrency(), "concurrency should be set to custom value")

		// Check cleanup interval is set correctly
		assert.Equal(cleanupInterval, w.Configs.CleanupCacheInterval, "cleanup interval should be set correctly")
	})

	t.Run("with direct concurrency value instead of ConfigFunc", func(t *testing.T) {
		// Create a simple worker function that returns void (WorkerFunc)
		voidFn := func(data string) {
			// Do nothing
		}

		// Set direct concurrency value
		concurrencyValue := 5

		// Create worker with direct concurrency value
		w := newWorker(voidFn, concurrencyValue)

		assert := assert.New(t)

		// Check concurrency is set correctly
		expectedConcurrency := concurrencyValue
		assert.Equal(expectedConcurrency, w.NumConcurrency(), "concurrency should be set to direct value")
	})

	t.Run("with custom job ID generator", func(t *testing.T) {
		// Create a simple worker function for void tasks
		voidFn := func(data string) {}

		// Create custom job ID generator
		customIdGenerator := func() string {
			return "test-id"
		}

		// Create worker with custom job ID generator
		w := newWorker(voidFn, WithJobIdGenerator(customIdGenerator))

		assert := assert.New(t)

		// Check job ID generator is set in the configs
		assert.NotNil(w.Configs.JobIdGenerator, "job ID generator should be set")
	})

	t.Run("with zero concurrency (should use CPU count)", func(t *testing.T) {
		// Create a simple worker function that returns void (WorkerFunc)
		voidFn := func(data string) {
			// Do nothing
		}

		// Create worker with zero concurrency
		w := newWorker(voidFn, WithConcurrency(0))

		assert := assert.New(t)

		// Check concurrency equals CPU count
		assert.Equal(withSafeConcurrency(0), w.concurrency.Load(), "concurrency should default to CPU count")
	})

	t.Run("with negative concurrency (should use CPU count)", func(t *testing.T) {
		// Create a simple worker function that returns void (WorkerFunc)
		voidFn := func(data string) {
			// Do nothing
		}

		// Create worker with negative concurrency
		w := newWorker(voidFn, WithConcurrency(-5))

		assert := assert.New(t)

		// Check concurrency equals CPU count
		assert.Equal(withSafeConcurrency(0), w.concurrency.Load(), "concurrency should default to CPU count with negative value")
	})

	t.Run("verify initial status is 'initiated'", func(t *testing.T) {
		// Create a simple worker function that returns void (WorkerFunc)
		voidFn := func(data string) {
			// Do nothing
		}

		// Create worker
		w := newWorker(voidFn)

		assert := assert.New(t)

		// Check status is 'initiated'
		assert.Equal(initiated, w.status.Load(), "Worker status should be 'initiated'")

		// Check the string representation of status
		assert.Equal("Initiated", w.Status(), "Worker status string should be 'Initiated'")
	})

	t.Run("with plain WorkerFunc and default pool initialization", func(t *testing.T) {
		// Create a worker function with no return value
		wf := func(data string) {
			// Simple worker function that does nothing
		}

		// Create standard worker
		w := newWorker(wf)

		assert := assert.New(t)

		// Validate worker structure
		assert.NotNil(w, "worker should not be nil")

		// Check pool is initialized
		assert.NotNil(w.pool, "worker pool should be initialized")
		assert.Zero(w.pool.Len(), "pool should be empty initially")

		// Check processing count is initialized to zero
		assert.Zero(w.CurProcessing.Load(), "current processing count should be initialized to zero")

		// Check that w.initPoolNode() creates a valid pool node
		node := w.initPoolNode()
		assert.NotNil(node, "initPoolNode should return a valid node")
		assert.NotNil(node.Value, "node value should not be nil")
	})

	t.Run("with WorkerFunc capturing closure variables", func(t *testing.T) {
		// Tests that worker functions can properly capture and modify variables in their closure
		counter := 0
		wf := func(data string) {
			counter += len(data)
		}

		// Create worker with this closure-using function
		w := newWorker(wf)
		assert := assert.New(t)
		assert.NotNil(w, "worker should not be nil")

		// We'll test the worker function execution in a more comprehensive worker state test
		// This test focuses on the initialization of the worker structure
		assert.NotNil(w.workerFunc, "worker function should be initialized")
	})

	t.Run("with newErrWorker and custom configuration", func(t *testing.T) {
		// Create a worker error function
		wf := func(data string) error {
			if len(data) == 0 {
				return errors.New("empty data")
			}
			return nil
		}

		// Set custom configurations
		customConcurrency := 3
		idleWorkerExpiryDuration := 5 * time.Minute
		minIdleWorkerRatio := uint8(20) // 20%

		// Create worker with multiple configurations
		w := newErrWorker[string](wf,
			WithConcurrency(customConcurrency),
			WithIdleWorkerExpiryDuration(idleWorkerExpiryDuration),
			WithMinIdleWorkerRatio(minIdleWorkerRatio),
		)

		assert := assert.New(t)

		// Validate worker structure
		assert.NotNil(w, "worker should not be nil")

		// Check worker function is set correctly
		assert.NotNil(w.workerFunc, "worker function should not be nil")

		// Check concurrency is set correctly
		assert.Equal(customConcurrency, w.NumConcurrency(), "concurrency should be set to custom value")

		// Check idle worker expiry duration is set correctly
		assert.Equal(idleWorkerExpiryDuration, w.Configs.IdleWorkerExpiryDuration, "idle worker expiry duration should be set correctly")

		// Check min idle worker ratio is set correctly
		assert.Equal(minIdleWorkerRatio, w.Configs.MinIdleWorkerRatio, "min idle worker ratio should be set correctly")

		// Check that numMinIdleWorkers calculates the correct number of idle workers
		expectedMinIdleWorkers := int(max((uint32(customConcurrency)*uint32(minIdleWorkerRatio))/100, 1))
		actualMinIdleWorkers := w.numMinIdleWorkers()
		assert.Equal(expectedMinIdleWorkers, actualMinIdleWorkers, "numMinIdleWorkers should return the expected value")
	})
}

func TestCurrentConcurrency(t *testing.T) {
	// Create a simple worker function that we'll use across tests
	voidFn := func(data string) {}

	t.Run("initial concurrency value", func(t *testing.T) {
		// Test with explicit concurrency value
		concurrencyValue := 4
		w := newWorker(voidFn, WithConcurrency(concurrencyValue))

		// Verify initial concurrency value
		assert.Equal(t, concurrencyValue, w.NumConcurrency(), "CurrentConcurrency should return the initial concurrency value")
	})

	t.Run("default concurrency value", func(t *testing.T) {
		// Create worker with default concurrency
		w := newWorker(voidFn)

		// Default concurrency should match the safe concurrency for 1
		expectedConcurrency := int(withSafeConcurrency(1))
		assert.Equal(t, expectedConcurrency, w.NumConcurrency(), "CurrentConcurrency should return the default concurrency value")
	})

	t.Run("concurrency after tuning", func(t *testing.T) {
		// Create worker with initial concurrency
		initialConcurrency := 2
		w := newWorker(voidFn, WithConcurrency(initialConcurrency))

		// Start the worker to allow tuning
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Verify initial concurrency
		assert.Equal(t, initialConcurrency, w.NumConcurrency(), "CurrentConcurrency should match initial value")

		// Tune concurrency
		newConcurrency := 5
		err = w.TunePool(newConcurrency)
		assert.NoError(t, err, "TunePool should not return error")

		// Verify concurrency was updated
		assert.Equal(t, newConcurrency, w.NumConcurrency(), "CurrentConcurrency should return updated value after tuning")
	})
}

func TestTunePool(t *testing.T) {
	// Create a simple worker function that we'll use across tests
	voidFn := func(data string) {
		// Do nothing
	}

	t.Run("worker not running error", func(t *testing.T) {
		// Create worker but don't start it
		w := newWorker(voidFn, WithConcurrency(2))

		// Try to tune concurrency on non-running worker
		err := w.TunePool(4)

		// Verify error is returned
		assert.Error(t, err, "TunePool should return error when worker is not running")
		assert.Equal(t, errNotRunningWorker, err, "Should return specific 'worker not running' error")
	})

	t.Run("increase concurrency", func(t *testing.T) {
		// Create worker with initial concurrency of 2
		initialConcurrency := 2
		w := newWorker(voidFn, WithConcurrency(initialConcurrency))

		// Initialize worker
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Confirm initial state
		assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")

		// Tune concurrency up to 5
		newConcurrency := 5
		err = w.TunePool(newConcurrency)
		assert.NoError(t, err, "TunePool should not return error on running worker")

		// Verify updated concurrency
		assert.Equal(t, newConcurrency, w.NumConcurrency(), "Concurrency should be updated to new value")

		// Allow time for channels to be created and added to stack
		time.Sleep(100 * time.Millisecond)

		// Check that new worker goroutines were started (channel stack will have more capacity)
		// We can't directly check stack size as channels are consumed in testing
		assert.Equal(t, newConcurrency, w.NumConcurrency(), "Stack should reflect the new concurrency")
	})

	t.Run("decrease concurrency", func(t *testing.T) {
		// Create worker with initial concurrency of 5
		initialConcurrency := 5
		w := newWorker(voidFn, WithConcurrency(initialConcurrency))

		// Initialize worker
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Confirm initial state
		assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")

		// Tune concurrency down to 2
		newConcurrency := 2
		err = w.TunePool(newConcurrency)
		assert.NoError(t, err, "TunePool should not return error on running worker")

		// Verify updated concurrency
		assert.Equal(t, newConcurrency, w.NumConcurrency(), "Concurrency should be updated to new lower value")

		// Allow time for channels to be closed
		time.Sleep(100 * time.Millisecond)

		// Verify the worker is still operational
		assert.True(t, w.IsRunning(), "Worker should still be running after decreasing concurrency")
	})

	t.Run("set concurrency to zero", func(t *testing.T) {
		// Create worker with initial concurrency of 3
		initialConcurrency := 3
		w := newWorker(voidFn, WithConcurrency(initialConcurrency))

		// Initialize worker
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Try to tune concurrency to 0 (should result in safe minimum concurrency)
		err = w.TunePool(0)
		assert.NoError(t, err, "TunePool should not return error on running worker")

		// Verify minimum safe concurrency is used instead of 0
		assert.Equal(t, withSafeConcurrency(0), w.concurrency.Load(), "Should use minimum safe concurrency when 0 is provided")
	})

	t.Run("set concurrency to negative value", func(t *testing.T) {
		// Create worker with initial concurrency of 3
		initialConcurrency := 3
		w := newWorker(voidFn, WithConcurrency(initialConcurrency))

		// Initialize worker
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Try to tune concurrency to -5 (should result in safe minimum concurrency)
		err = w.TunePool(-5)
		assert.NoError(t, err, "TunePool should not return error on running worker")

		// Verify minimum safe concurrency is used instead of negative value
		assert.Equal(t, withSafeConcurrency(-5), w.concurrency.Load(), "Should use minimum safe concurrency when negative value is provided")
	})

	t.Run("same concurrency value", func(t *testing.T) {
		// Create worker with initial concurrency of 4
		initialConcurrency := 4
		w := newWorker(voidFn, WithConcurrency(initialConcurrency))

		// Initialize worker
		err := w.start()
		assert.NoError(t, err, "Worker should start without error")
		defer w.Stop() // Clean up

		// Confirm initial state
		assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")

		// "Tune" to the same concurrency value
		err = w.TunePool(initialConcurrency)
		assert.ErrorIs(t, err, errSameConcurrency, "TunePool should return error when concurrency is the same")

		// Verify concurrency remains unchanged
		assert.Equal(t, initialConcurrency, w.NumConcurrency(), "Concurrency should remain unchanged when set to same value")
	})
}

func TestWorkerLifecycle(t *testing.T) {
	t.Run("worker state transitions", func(t *testing.T) {
		// Create a simple worker function
		voidFn := func(data string) {
			time.Sleep(10 * time.Millisecond) // Short sleep to simulate work
		}

		// Create worker with default config
		w := newWorker(voidFn)
		assert := assert.New(t)

		// Test initial state
		assert.Equal(initiated, w.status.Load(), "Initial status should be 'initiated'")
		assert.Equal("Initiated", w.Status(), "Initial status string should be 'Initiated'")
		assert.False(w.IsRunning(), "Worker should not be running initially")
		assert.False(w.IsPaused(), "Worker should not be paused initially")
		assert.False(w.IsStopped(), "Worker should not be stopped initially")

		// Test start
		err := w.start()
		assert.NoError(err, "Starting worker should not error")
		assert.Equal(running, w.status.Load(), "Status after start should be 'running'")
		assert.Equal("Running", w.Status(), "Status string after start should be 'Running'")
		assert.True(w.IsRunning(), "Worker should be running after start")

		// Test running error when already running
		err = w.start()
		assert.ErrorIs(err, errRunningWorker, "Starting an already running worker should error")

		// Test pause
		w.Pause()
		assert.Equal(paused, w.status.Load(), "Status after pause should be 'paused'")
		assert.Equal("Paused", w.Status(), "Status string after pause should be 'Paused'")
		assert.True(w.IsPaused(), "Worker should be paused after Pause()")
		assert.False(w.IsRunning(), "Worker should not be running after Pause()")

		// Test resume
		err = w.Resume()
		assert.NoError(err, "Resuming worker should not error")
		assert.Equal(running, w.status.Load(), "Status after resume should be 'running'")
		assert.Equal("Running", w.Status(), "Status string after resume should be 'Running'")
		assert.True(w.IsRunning(), "Worker should be running after Resume()")

		// Test resume error when already running
		err = w.Resume()
		assert.ErrorIs(err, errRunningWorker, "Resuming an already running worker should error")

		// Test stop
		w.Stop()
		assert.Equal(stopped, w.status.Load(), "Status after stop should be 'stopped'")
		assert.Equal("Stopped", w.Status(), "Status string after stop should be 'Stopped'")
		assert.True(w.IsStopped(), "Worker should be stopped after Stop()")
		assert.False(w.IsRunning(), "Worker should not be running after Stop()")
		assert.False(w.IsPaused(), "Worker should not be paused after Stop()")
	})

	t.Run("pause and wait functionality", func(t *testing.T) {
		// Track job processing
		var jobsProcessed atomic.Uint32

		// Create a worker function that increments counter
		voidFn := func(data string) {
			time.Sleep(30 * time.Millisecond) // Simulate work
			jobsProcessed.Add(1)
		}

		// Create worker with concurrency 2
		w := newWorker(voidFn)
		assert := assert.New(t)

		// Create a queue for testing using internal implementation
		q := queues.NewQueue[iJob[string]]()
		w.setQueue(q)

		// Submit some jobs
		for i := range 10 {
			q.Enqueue(newJob(string(rune('0'+i)), loadJobConfigs(w.configs())))
		}

		// Start worker
		err := w.start()
		assert.NoError(err, "Starting worker should not error")

		w.PauseAndWait()

		// Check no jobs are being processed
		assert.Zero(w.NumProcessing(), "No jobs should be processing after PauseAndWait")

		// Check status
		assert.True(w.IsPaused(), "Worker should be paused after PauseAndWait")

		// Keep track of processed count before resume
		processedBeforeResume := jobsProcessed.Load()

		// Resume and let remaining jobs process
		err = w.Resume()
		assert.NoError(err, "Resuming worker should not error")

		time.Sleep(200 * time.Millisecond)
		// Check more jobs were processed after resume
		assert.Greater(jobsProcessed.Load(), processedBeforeResume, "More jobs should be processed after resume")

		// Clean up
		w.Stop()
	})
}
