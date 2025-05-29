package varmq

import (
	"errors"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goptics/varmq/internal/queues"
	"github.com/stretchr/testify/assert"
)

// TestWorkerGroup demonstrates the test group pattern for worker tests
func TestWorkers(t *testing.T) {
	// Main test function that groups all worker tests

	t.Run("BasicWorker", func(t *testing.T) {
		// Group 1: Initialization tests
		t.Run("Initialization", func(t *testing.T) {
			t.Run("with default configuration", func(t *testing.T) {
				// Create worker with default config
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				})
				assert := assert.New(t)

				// Validate worker structure
				assert.NotNil(w, "worker should not be nil")
				assert.NotNil(w.workerFunc, "worker function should not be nil")
				assert.Equal(1, w.NumConcurrency(), "concurrency should match expected value")
				assert.Equal(initiated, w.status.Load(), "status should be 'initiated'")
				assert.NotNil(w.Queue, "queue should not be nil, expected null queue")
				assert.False(reflect.ValueOf(w.eventLoopSignal).IsNil(), "eventLoopSignal should be initialized")
				assert.NotNil(w.tickers, "tickers map should be initialized")
				assert.NotNil(w.waiters, "waiters slice should be initialized")
				assert.NotNil(w.pool, "worker pool should be initialized")
				assert.Zero(w.pool.Len(), "pool should be empty initially")
				assert.Zero(w.curProcessing.Load(), "current processing count should be initialized to zero")
			})

			t.Run("with direct concurrency value", func(t *testing.T) {
				concurrencyValue := 5
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, concurrencyValue)
				assert := assert.New(t)
				assert.Equal(concurrencyValue, w.NumConcurrency(), "concurrency should be set to direct value")
			})

			t.Run("with custom job ID generator", func(t *testing.T) {
				customIdGenerator := func() string {
					return "test-id"
				}
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithJobIdGenerator(customIdGenerator))
				assert := assert.New(t)
				assert.NotNil(w.Configs.JobIdGenerator, "job ID generator should be set")
			})

			t.Run("with zero concurrency (should use CPU count)", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithConcurrency(0))
				assert := assert.New(t)
				assert.Equal(withSafeConcurrency(0), w.concurrency.Load(), "concurrency should default to CPU count")
			})

			t.Run("with negative concurrency (should use CPU count)", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithConcurrency(-5))
				assert := assert.New(t)
				assert.Equal(withSafeConcurrency(0), w.concurrency.Load(), "concurrency should default to CPU count with negative value")
			})

			t.Run("with closure capturing", func(t *testing.T) {
				counter := 0
				wf := func(j iJob[string]) {
					counter += len(j.Data())
				}
				w := newWorker(wf)
				assert := assert.New(t)
				assert.NotNil(w, "worker should not be nil")
				assert.NotNil(w.workerFunc, "worker function should be initialized")
			})

			t.Run("pool node initialization", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				})
				assert := assert.New(t)
				node := w.initPoolNode()
				assert.NotNil(node, "initPoolNode should return a valid node")
				assert.NotNil(node.Value, "node value should not be nil")
			})
		})

		// Group 2: Concurrency tests
		t.Run("Concurrency", func(t *testing.T) {
			t.Run("initial value", func(t *testing.T) {
				concurrencyValue := 4
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithConcurrency(concurrencyValue))
				assert.Equal(t, concurrencyValue, w.NumConcurrency(), "CurrentConcurrency should return the initial concurrency value")
			})

			t.Run("default value", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				})
				expectedConcurrency := int(withSafeConcurrency(1))
				assert.Equal(t, expectedConcurrency, w.NumConcurrency(), "CurrentConcurrency should return the default concurrency value")
			})

			t.Run("after tuning", func(t *testing.T) {
				initialConcurrency := 2
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithConcurrency(initialConcurrency))
				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				assert.Equal(t, initialConcurrency, w.NumConcurrency(), "CurrentConcurrency should match initial value")

				newConcurrency := 5
				err = w.TunePool(newConcurrency)
				assert.NoError(t, err, "TunePool should not return error")

				assert.Equal(t, newConcurrency, w.NumConcurrency(), "CurrentConcurrency should return updated value after tuning")
			})
		})

		// Group 3: Pool tuning tests
		t.Run("PoolTuning", func(t *testing.T) {
			t.Run("worker not running error", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithConcurrency(2))
				err := w.TunePool(4)
				assert.Error(t, err, "TunePool should return error when worker is not running")
				assert.Equal(t, errNotRunningWorker, err, "Should return specific 'worker not running' error")
			})

			t.Run("increase concurrency", func(t *testing.T) {
				initialConcurrency := 2
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithConcurrency(initialConcurrency))
				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")

				newConcurrency := 5
				err = w.TunePool(newConcurrency)
				assert.NoError(t, err, "TunePool should not return error on running worker")

				assert.Equal(t, newConcurrency, w.NumConcurrency(), "Concurrency should be updated to new value")
				time.Sleep(100 * time.Millisecond)
				assert.Equal(t, newConcurrency, w.NumConcurrency(), "Stack should reflect the new concurrency")
			})

			t.Run("decrease concurrency", func(t *testing.T) {
				initialConcurrency := 5
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithConcurrency(initialConcurrency))
				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")

				newConcurrency := 2
				err = w.TunePool(newConcurrency)
				assert.NoError(t, err, "TunePool should not return error on running worker")

				assert.Equal(t, newConcurrency, w.NumConcurrency(), "Concurrency should be updated to new lower value")
				time.Sleep(100 * time.Millisecond)
				assert.True(t, w.IsRunning(), "Worker should still be running after decreasing concurrency")
			})

			t.Run("set concurrency to zero", func(t *testing.T) {
				initialConcurrency := 3
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithConcurrency(initialConcurrency))
				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				err = w.TunePool(0)
				assert.NoError(t, err, "TunePool should not return error on running worker")
				assert.Equal(t, withSafeConcurrency(0), w.concurrency.Load(), "Should use minimum safe concurrency when 0 is provided")
			})

			t.Run("set concurrency to negative value", func(t *testing.T) {
				initialConcurrency := 3
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithConcurrency(initialConcurrency))
				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				err = w.TunePool(-5)
				assert.NoError(t, err, "TunePool should not return error on running worker")
				assert.Equal(t, withSafeConcurrency(-5), w.concurrency.Load(), "Should use minimum safe concurrency when negative value is provided")
			})

			t.Run("same concurrency value", func(t *testing.T) {
				initialConcurrency := 4
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithConcurrency(initialConcurrency))
				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(), "Initial concurrency should be set correctly")
				err = w.TunePool(initialConcurrency)
				assert.ErrorIs(t, err, errSameConcurrency, "TunePool should return error when concurrency is the same")
				assert.Equal(t, initialConcurrency, w.NumConcurrency(), "Concurrency should remain unchanged when set to same value")
			})

			t.Run("decrease concurrency with idle worker expiry duration", func(t *testing.T) {
				initialConcurrency := 10
				idleExpiryDuration := 100 * time.Millisecond

				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				},
					WithConcurrency(initialConcurrency),
					WithIdleWorkerExpiryDuration(idleExpiryDuration),
				)

				// Create a queue for testing
				q := queues.NewQueue[iJob[string]]()
				w.setQueue(q)
				defer w.Stop()

				// Verify IdleWorkerExpiryDuration is set
				assert.Equal(t, idleExpiryDuration, w.Configs.IdleWorkerExpiryDuration,
					"IdleWorkerExpiryDuration should be set to specified value")
				assert.Equal(t, uint32(initialConcurrency), w.concurrency.Load(),
					"Initial concurrency should be set correctly")

				// Add jobs to expand the worker pool to its maximum size
				for i := range initialConcurrency * 2 {
					q.Enqueue(newJob("job"+string(rune(i)), loadJobConfigs(w.configs())))
				}

				err := w.start()
				assert := assert.New(t)
				assert.NoError(err, "Worker should start without error")

				// Wait for jobs to be processed and pool to expand
				w.WaitUntilFinished()

				assert.Equal(w.pool.Len(), w.NumConcurrency(), "Pool size should be equal to the concurrency")

				// Decrease concurrency
				newConcurrency := 3
				err = w.TunePool(newConcurrency)
				assert.NoError(err, "TunePool should not return error when decreasing concurrency")

				// Concurrency should be updated but pool size should not be manually shrunk
				assert.Equal(uint32(newConcurrency), w.concurrency.Load(),
					"Concurrency should be updated to new lower value")

				assert.Greater(w.pool.Len(), newConcurrency, "Pool size should be greater than the concurrency")
				time.Sleep(idleExpiryDuration * 2)
				// After the expiration the pool size must be equal to one
				assert.Equal(w.pool.Len(), 1, "Pool size should be equal to one after expiration")
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
				worker.WaitUntilFinished()

				// After waiting, should have no pending jobs
				assert.Equal(0, queue.NumPending(), "Queue should have no pending jobs after WaitUntilFinished")
				assert.Equal(0, internalQueue.Len(), "Internal queue should be empty after WaitUntilFinished")
			})

			t.Run("WaitAndStop", func(t *testing.T) {
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
				err = worker.WaitAndStop()
				assert.NoError(err, "WaitAndStop should not return an error")

				// After waiting and closing, should have no pending jobs and worker should be stopped
				assert.Equal(0, queue.NumPending(), "Queue should have no pending jobs after WaitAndStop")
				assert.Equal(0, internalQueue.Len(), "Internal queue should be empty after WaitAndStop")
				assert.True(worker.IsStopped(), "Worker should be stopped after WaitAndStop")
			})
		})

		// Group 4: Lifecycle tests
		t.Run("Lifecycle", func(t *testing.T) {
			t.Run("worker state transitions", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				})
				assert := assert.New(t)

				assert.Equal(initiated, w.status.Load(), "Initial status should be 'initiated'")
				assert.Equal("Initiated", w.Status(), "Initial status string should be 'Initiated'")
				assert.False(w.IsRunning(), "Worker should not be running initially")
				assert.False(w.IsPaused(), "Worker should not be paused initially")
				assert.False(w.IsStopped(), "Worker should not be stopped initially")

				err := w.start()
				assert.NoError(err, "Starting worker should not error")
				assert.Equal(running, w.status.Load(), "Status after start should be 'running'")
				assert.Equal("Running", w.Status(), "Status string after start should be 'Running'")
				assert.True(w.IsRunning(), "Worker should be running after start")

				err = w.start()
				assert.ErrorIs(err, errRunningWorker, "Starting an already running worker should error")

				w.Pause()
				assert.Equal(paused, w.status.Load(), "Status after pause should be 'paused'")
				assert.Equal("Paused", w.Status(), "Status string after pause should be 'Paused'")
				assert.True(w.IsPaused(), "Worker should be paused after Pause()")
				assert.False(w.IsRunning(), "Worker should not be running after Pause()")

				err = w.Resume()
				assert.NoError(err, "Resuming worker should not error")
				assert.Equal(running, w.status.Load(), "Status after resume should be 'running'")
				assert.Equal("Running", w.Status(), "Status string after resume should be 'Running'")
				assert.True(w.IsRunning(), "Worker should be running after Resume()")

				err = w.Resume()
				assert.ErrorIs(err, errRunningWorker, "Resuming an already running worker should error")

				w.Stop()
				assert.Equal(stopped, w.status.Load(), "Status after stop should be 'stopped'")
				assert.Equal("Stopped", w.Status(), "Status string after stop should be 'Stopped'")
				assert.True(w.IsStopped(), "Worker should be stopped after Stop()")
				assert.False(w.IsRunning(), "Worker should not be running after Stop()")
				assert.False(w.IsPaused(), "Worker should not be paused after Stop()")

				// Test the "Unknown" status case
				// This is a direct manipulation of internal state for testing purposes
				w.status.Store(99) // Invalid status value
				assert.Equal("Unknown", w.Status(), "Status string for unknown status should be 'Unknown'")
			})

			t.Run("pause and wait functionality", func(t *testing.T) {
				// Track job processing
				var jobsProcessed atomic.Uint32

				// Create a worker function that increments counter
				fn := func(j iJob[int]) {
					time.Sleep(10 * time.Millisecond) // Simulate work
					jobsProcessed.Add(1)
				}

				// Create worker with concurrency 2
				w := newWorker(fn)
				assert := assert.New(t)

				// Create a queue for testing using internal implementation
				q := queues.NewQueue[iJob[int]]()
				w.setQueue(q)

				// Submit some jobs
				for i := range 10 {
					q.Enqueue(newJob(i, loadJobConfigs(w.configs())))
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

				time.Sleep(100 * time.Millisecond)
				// Check more jobs were processed after resume
				assert.Greater(jobsProcessed.Load(), processedBeforeResume, "More jobs should be processed after resume")
				time.Sleep(100 * time.Millisecond)

				// should process all jobs
				assert.Equal(jobsProcessed.Load(), uint32(10), "All jobs should be processed after resume")

				w.Stop()
			})

			t.Run("restart functionality", func(t *testing.T) {
				// Track job processing
				var jobsProcessed atomic.Uint32

				// Create a worker function that increments counter
				workerFn := func(j iJob[int]) {
					time.Sleep(5 * time.Millisecond) // Simulate work
					jobsProcessed.Add(1)
				}

				w := newWorker(workerFn)
				assert := assert.New(t)

				// Create a queue for testing
				q := queues.NewQueue[iJob[int]]()
				w.setQueue(q)

				// Submit some initial jobs
				for i := range 50 {
					q.Enqueue(newJob(i, loadJobConfigs(w.configs())))
				}

				// Start worker
				err := w.start()
				assert.NoError(err, "Starting worker should not error")
				assert.True(w.IsRunning(), "Worker should be running after start")

				// Wait for some jobs to be processed
				time.Sleep(50 * time.Millisecond)

				// Store the state before restart
				processedBeforeRestart := jobsProcessed.Load()
				assert.Greater(processedBeforeRestart, uint32(0), "Some jobs should be processed before restart")

				// Verify eventLoopSignal exists before restart
				assert.False(reflect.ValueOf(w.eventLoopSignal).IsNil(), "eventLoopSignal should exist before restart")

				// Restart the worker
				err = w.Restart()
				assert.NoError(err, "Restarting worker should not error")

				// Verify worker is running after restart
				assert.True(w.IsRunning(), "Worker should be running after restart")

				// Verify the eventLoopSignal was recreated
				assert.False(reflect.ValueOf(w.eventLoopSignal).IsNil(), "eventLoopSignal should be recreated after restart")

				// Wait for jobs to be processed after restart
				time.Sleep(50 * time.Millisecond)

				// Verify more jobs were processed after restart
				assert.Greater(jobsProcessed.Load(), processedBeforeRestart, "More jobs should be processed after restart")

				// Clean up
				w.Stop()
			})
		})

		// Group 5: Pool management tests
		t.Run("PoolManagement", func(t *testing.T) {
			t.Run("numMinIdleWorkers calculation", func(t *testing.T) {
				testCases := []struct {
					concurrency int
					ratio       uint8
					expected    int
				}{
					{concurrency: 10, ratio: 10, expected: 1},   // 10% of 10 = 1
					{concurrency: 10, ratio: 20, expected: 2},   // 20% of 10 = 2
					{concurrency: 5, ratio: 30, expected: 1},    // 30% of 5 = 1.5, rounded to 1
					{concurrency: 100, ratio: 15, expected: 15}, // 15% of 100 = 15
					{concurrency: 3, ratio: 50, expected: 1},    // 50% of 3 = 1.5, rounded to 1
					{concurrency: 1, ratio: 100, expected: 1},   // 100% of 1 = 1
				}

				for _, tc := range testCases {
					w := newWorker(func(j iJob[string]) {
						time.Sleep(10 * time.Millisecond)
					},
						WithConcurrency(tc.concurrency),
						WithMinIdleWorkerRatio(tc.ratio),
					)

					assert := assert.New(t)
					actual := w.numMinIdleWorkers()
					assert.Equal(tc.expected, actual,
						"numMinIdleWorkers should return %d for concurrency=%d and ratio=%d",
						tc.expected, tc.concurrency, tc.ratio)
				}
			})

			t.Run("idle worker management", func(t *testing.T) {
				expirySetting := 50 * time.Millisecond
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				},
					WithConcurrency(10),
					WithIdleWorkerExpiryDuration(expirySetting),
					WithMinIdleWorkerRatio(50),
				)
				defer w.Stop()
				assert := assert.New(t)

				err := w.start()
				assert.NoError(err, "Starting worker should not error")

				initialCount := w.pool.Len()
				assert.Equal(initialCount, 1, "Should have one idle worker in the pool after start")

				for range 9 {
					node := w.initPoolNode()
					node.Value.UpdateLastUsed()
					w.pool.PushNode(node)
				}

				time.Sleep(expirySetting * 2)
				assert.Less(w.NumIdleWorkers(), w.NumConcurrency(),
					"Number of idle workers should be reduced to less than concurrency")
				assert.Equal(w.NumIdleWorkers(), w.numMinIdleWorkers(),
					"Number of idle workers should be equal to min idle workers")
			})
		})
	})

	t.Run("ResultWorker", func(t *testing.T) {
		// Group 1: Initialization tests
		t.Run("Initialization", func(t *testing.T) {
			t.Run("with default configuration", func(t *testing.T) {
				w := newResultWorker(func(j iResultJob[string, int]) {
					j.sendResult(len(j.Data()))
				})
				assert := assert.New(t)
				assert.NotNil(w, "worker should not be nil")
				assert.NotNil(w.workerFunc, "worker function should not be nil")
				assert.Equal(1, w.NumConcurrency(), "concurrency should match expected value")
			})

			t.Run("with custom concurrency", func(t *testing.T) {
				customConcurrency := 4
				w := newResultWorker(func(j iResultJob[string, int]) {
					j.sendResult(len(j.Data()))
				}, WithConcurrency(customConcurrency))
				assert := assert.New(t)
				assert.Equal(customConcurrency, w.NumConcurrency(), "concurrency should be set to custom value")
			})
		})
	})

	t.Run("ErrorWorker", func(t *testing.T) {
		// Group 1: Initialization tests
		t.Run("Initialization", func(t *testing.T) {
			t.Run("with default configuration", func(t *testing.T) {
				w := newErrWorker(func(j iErrorJob[string]) {
					j.sendError(nil)
				})
				assert := assert.New(t)
				assert.NotNil(w, "worker should not be nil")
				assert.NotNil(w.workerFunc, "worker function should not be nil")
				assert.Equal(1, w.NumConcurrency(), "concurrency should match expected value")
			})

			t.Run("with custom configuration", func(t *testing.T) {
				wf := func(j iErrorJob[string]) {
					if len(j.Data()) == 0 {
						j.sendError(errors.New("empty data"))
					}
				}

				customConcurrency := 3
				idleWorkerExpiryDuration := 5 * time.Minute
				minIdleWorkerRatio := uint8(20) // 20%

				w := newErrWorker(wf,
					WithConcurrency(customConcurrency),
					WithIdleWorkerExpiryDuration(idleWorkerExpiryDuration),
					WithMinIdleWorkerRatio(minIdleWorkerRatio),
				)

				assert := assert.New(t)
				assert.NotNil(w, "worker should not be nil")
				assert.NotNil(w.workerFunc, "worker function should not be nil")
				assert.Equal(customConcurrency, w.NumConcurrency(), "concurrency should be set to custom value")
				assert.Equal(idleWorkerExpiryDuration, w.Configs.IdleWorkerExpiryDuration, "idle worker expiry duration should be set correctly")
				assert.Equal(minIdleWorkerRatio, w.Configs.MinIdleWorkerRatio, "min idle worker ratio should be set correctly")

				expectedMinIdleWorkers := int(max((uint32(customConcurrency)*uint32(minIdleWorkerRatio))/100, 1))
				actualMinIdleWorkers := w.numMinIdleWorkers()
				assert.Equal(expectedMinIdleWorkers, actualMinIdleWorkers, "numMinIdleWorkers should return the expected value")
			})
		})
	})
}

// TestWorkerBinders tests the worker binder implementations
func TestWorkerBinders(t *testing.T) {
	// Test standard worker binder
	t.Run("WorkerBinder_BindQueue", func(t *testing.T) {
		// Create a worker
		w := newWorker(func(j iJob[string]) {})

		// Create a worker binder
		binder := newQueues(w)

		// Test BindQueue
		queue := binder.BindQueue()
		assert.NotNil(t, queue, "Queue should not be nil")

		// Verify queue is not nil
		assert.NotNil(t, queue, "Queue should not be nil")
		assert.True(t, w.IsRunning(), "Worker should be running after binding queue")

		// Clean up
		w.Stop()
	})

	t.Run("WorkerBinder_BindPriorityQueue", func(t *testing.T) {
		// Create a worker
		w := newWorker(func(j iJob[string]) {})

		// Create a worker binder
		binder := newQueues(w)

		// Test BindPriorityQueue
		pQueue := binder.BindPriorityQueue()
		assert.NotNil(t, pQueue, "PriorityQueue should not be nil")

		// Verify priority queue is not nil
		assert.NotNil(t, pQueue, "PriorityQueue should not be nil")
		assert.True(t, w.IsRunning(), "Worker should be running after binding priority queue")

		// Clean up
		w.Stop()
	})

	t.Run("WorkerBinder_HasDistributedQueueMethod", func(t *testing.T) {
		// Create a worker
		w := newWorker(func(j iJob[string]) {})

		// Create a worker binder
		binder := newQueues(w)

		// Use reflection to check if the method exists
		binderType := reflect.TypeOf(binder)
		_, exists := binderType.MethodByName("WithDistributedQueue")
		assert.True(t, exists, "WorkerBinder should have WithDistributedQueue method")

		_, exists = binderType.MethodByName("WithDistributedPriorityQueue")
		assert.True(t, exists, "WorkerBinder should have WithDistributedPriorityQueue method")

		// No need to clean up as worker wasn't started
	})

	t.Run("WorkerBinder_HasPersistentQueueMethod", func(t *testing.T) {
		// Create a worker
		w := newWorker(func(j iJob[string]) {})

		// Create a worker binder
		binder := newQueues(w)

		// Use reflection to check if the method exists
		binderType := reflect.TypeOf(binder)
		_, exists := binderType.MethodByName("WithPersistentQueue")
		assert.True(t, exists, "WorkerBinder should have WithPersistentQueue method")

		_, exists = binderType.MethodByName("WithPersistentPriorityQueue")
		assert.True(t, exists, "WorkerBinder should have WithPersistentPriorityQueue method")

		// No need to clean up as worker wasn't started
	})

	t.Run("ResultWorkerBinder_HasQueueMethods", func(t *testing.T) {
		// Create a result worker
		w := newResultWorker(func(j iResultJob[string, int]) {
			j.sendResult(len(j.Data()))
		})

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

		// No need to clean up as worker wasn't started
	})

	t.Run("ErrWorkerBinder_HasQueueMethods", func(t *testing.T) {
		// Create an error worker
		w := newErrWorker(func(j iErrorJob[string]) {
			j.sendError(nil)
		})

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

		// No need to clean up as worker wasn't started
	})

	// Test result worker binder
	t.Run("ResultWorkerBinder_BindQueue", func(t *testing.T) {
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
		assert.True(t, w.IsRunning(), "Worker should be running after binding queue")

		// Clean up
		w.Stop()
	})

	t.Run("ResultWorkerBinder_BindPriorityQueue", func(t *testing.T) {
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
		assert.True(t, w.IsRunning(), "Worker should be running after binding priority queue")

		// Clean up
		w.Stop()
	})

	// Test error worker binder
	t.Run("ErrWorkerBinder_BindQueue", func(t *testing.T) {
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
		assert.True(t, w.IsRunning(), "Worker should be running after binding queue")

		// Clean up
		w.Stop()
	})

	t.Run("ErrWorkerBinder_BindPriorityQueue", func(t *testing.T) {
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
		assert.True(t, w.IsRunning(), "Worker should be running after binding priority queue")

		// Clean up
		w.Stop()
	})
}
