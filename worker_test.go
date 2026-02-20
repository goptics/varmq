package varmq

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goptics/varmq/internal/queues"
	"github.com/goptics/varmq/mocks"
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
				assert.NotNil(&w.queues, "queues should not be nil, expected null queue")
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
				assert.NotNil(w.Configs.jobIdGenerator, "job ID generator should be set")
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
				assert.Equal(t, ErrNotRunningWorker, err, "Should return specific 'worker not running' error")
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
				assert.ErrorIs(t, err, ErrSameConcurrency, "TunePool should return error when concurrency is the same")
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
				w.queues.Register(q)
				defer w.Stop()

				// Verify idleWorkerExpiryDuration is set
				assert.Equal(t, idleExpiryDuration, w.Configs.idleWorkerExpiryDuration,
					"idleWorkerExpiryDuration should be set to specified value")
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
				assert.ErrorIs(err, ErrRunningWorker, "Starting an already running worker should error")

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
				assert.ErrorIs(err, ErrRunningWorker, "Resuming an already running worker should error")

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
				w.queues.Register(q)

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
				w.queues.Register(q)

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

			t.Run("restart after stop", func(t *testing.T) {
				w := newWorker(func(j iJob[int]) {
					time.Sleep(5 * time.Millisecond)
				})
				assert := assert.New(t)

				// Start and stop
				err := w.start()
				assert.NoError(err)
				w.Stop()
				assert.True(w.IsStopped())

				// Restart
				err = w.Restart()
				assert.NoError(err, "Restarting a stopped worker should succeed")
				assert.True(w.IsRunning(), "Worker should be running after restart")

				// Cleanup
				w.Stop()
			})

			t.Run("stop after pause", func(t *testing.T) {
				w := newWorker(func(j iJob[int]) {
					time.Sleep(5 * time.Millisecond)
				})
				assert := assert.New(t)

				// Start and pause
				err := w.start()
				assert.NoError(err)
				w.Pause()
				assert.True(w.IsPaused())

				// Stop
				err = w.Stop()
				assert.NoError(err, "Stopping a paused worker should succeed")
				assert.True(w.IsStopped(), "Worker should be stopped")
			})

			t.Run("event loop processing error", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {})
				errChan := w.Errs()

				// Register a mock queue that fails dequeue
				mockQueue := mocks.NewMockPersistentQueue()
				mockQueue.ShouldFailDequeue = true
				// Need to put something in it to trigger the loop
				mockQueue.Queue.Enqueue("trigger")

				w.queues.Register(newQueue(w, mockQueue).internalQueue)

				w.start()
				defer w.Stop()

				select {
				case err := <-errChan:
					assert.ErrorIs(t, err, ErrFailedToDequeue)
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for error from event loop")
				}
			})

			t.Run("redundant state transitions", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {})
				// initiated to paused: error
				assert.ErrorIs(t, w.Pause(), ErrNotRunningWorker)

				w.start()
				w.Pause()
				assert.True(t, w.IsPaused())
				// paused to paused: nil
				assert.NoError(t, w.Pause())

				w.Stop()
				assert.True(t, w.IsStopped())
				// stopped to paused: nil
				assert.NoError(t, w.Pause())
				// stopped to stopped: nil
				assert.NoError(t, w.Stop())
			})

			t.Run("restart state conditions", func(t *testing.T) {
				t.Run("RestartRunning", func(t *testing.T) {
					w := newWorker(func(j iJob[string]) {
						time.Sleep(10 * time.Millisecond)
					})
					w.start()
					assert.NoError(t, w.Restart())
					assert.True(t, w.IsRunning())
					w.Stop()
				})

				t.Run("RestartPaused", func(t *testing.T) {
					w := newWorker(func(j iJob[string]) {})
					w.start()
					w.Pause()
					assert.NoError(t, w.Restart())
					assert.True(t, w.IsRunning())
					w.Stop()
				})

				t.Run("RestartDefaultCase", func(t *testing.T) {
					w := newWorker(func(j iJob[string]) {})
					// Manually set an invalid status to hit default case in switch
					w.status.Store(99)
					assert.ErrorIs(t, w.Restart(), ErrNotRunningWorker)
				})

				t.Run("RestartPauseAndWaitError", func(t *testing.T) {
					w := newWorker(func(j iJob[string]) {})
					defer w.Stop()

					done := make(chan struct{})
					// Use many goroutines to increase the race probability
					for range 50 {
						go func() {
							for {
								select {
								case <-done:
									return
								default:
									if w.status.Load() == running {
										w.status.Store(initiated)
									}
									runtime.Gosched()
								}
							}
						}()
					}

					// Try many times to hit the race
					for range 5000 {
						w.status.Store(running)
						_ = w.Restart()
					}
					close(done)
				})

				t.Run("RestartStartError", func(t *testing.T) {
					w := newWorker(func(j iJob[string]) {})
					defer w.Stop()

					w.status.Store(stopped)

					done := make(chan struct{})
					for range 50 {
						go func() {
							for {
								select {
								case <-done:
									return
								default:
									if w.status.Load() == initiated {
										w.status.Store(running)
									}
									runtime.Gosched()
								}
							}
						}()
					}

					for range 500 {
						w.status.Store(stopped)
						_ = w.Restart()
					}
					close(done)
				})
			})

			t.Run("pool node job close error", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {})
				w.status.Store(running)

				// Define a local mock job that fails on Close
				mj := &mockJob{
					job: newJob("test", loadJobConfigs(w.configs())),
				}
				mj.shouldFailClose = true

				// Trigger pool node execution
				node := w.initPoolNode()
				node.Value.Send(mj)

				// Wait for error
				select {
				case err := <-w.Errs():
					assert.Error(t, err)
					assert.Equal(t, "close failure", err.Error())
				case <-time.After(500 * time.Millisecond):
					t.Fatal("timed out waiting for close error")
				}
			})
		})

		// Group 5: Status checks
		t.Run("StatusChecks", func(t *testing.T) {
			t.Run("NumPending", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				})
				assert := assert.New(t)

				// Create a queue
				q := queues.NewQueue[iJob[string]]()
				w.queues.Register(q)

				// Initial check
				assert.Equal(0, w.NumPending(), "NumPending should be 0 initially")

				// Add jobs
				jobCount := 5
				for i := range jobCount {
					q.Enqueue(newJob("job"+strconv.Itoa(i), loadJobConfigs(w.configs())))
				}

				// Check NumPending reflects the queue length
				assert.Equal(jobCount, w.NumPending(), "NumPending should reflect the number of jobs in the queue")

				// Start worker to process jobs
				err := w.start()
				assert.NoError(err, "Worker should start without error")
				defer w.Stop()

				// Wait for processing
				w.WaitUntilFinished()

				// Check NumPending is 0
				assert.Equal(0, w.NumPending(), "NumPending should be 0 after processing")
			})
		})

		// Group 6: Pool management tests
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

		// Group 7: Edge cases and error handling tests
		t.Run("EdgeCases", func(t *testing.T) {
			t.Run("processNextJob with IAcknowledgeable queue", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				// Create a persistent queue that implements IAcknowledgeable
				persistentQueue := mocks.NewMockPersistentQueue()
				w.queues.Register(persistentQueue)
				job := newJob("test-data", loadJobConfigs(w.configs()))
				persistentQueue.Enqueue(job)

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				// Process should handle queue
				w.processNextJob()
				time.Sleep(10 * time.Millisecond)

				assert.Equal(t, 0, persistentQueue.Len(), "Item should be processed from queue")
			})

			t.Run("processNextJob with byte data parsing", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				// Create a queue with byte data
				job := newJob("test-data", loadJobConfigs(w.configs()))
				jobBytes, _ := job.Json()

				testQueue := queues.NewQueue[any]()
				testQueue.Enqueue(jobBytes)
				w.queues.Register(testQueue)

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				// Process should handle byte parsing
				w.processNextJob()
				time.Sleep(10 * time.Millisecond)

				assert.Equal(t, 0, testQueue.Len(), "Byte data should be parsed and processed")
			})

			t.Run("processNextJob with closed job", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				// Create a closed job
				job := newJob("test-data", loadJobConfigs(w.configs()))
				job.Close()

				testQueue := queues.NewQueue[any]()
				testQueue.Enqueue(job)
				w.queues.Register(testQueue)

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				// Process should skip closed job
				w.processNextJob()
				time.Sleep(10 * time.Millisecond)

				assert.Equal(t, 0, testQueue.Len(), "Closed job should be dequeued but not processed")
			})

			t.Run("processNextJob with invalid byte data", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				// Create invalid byte data
				testQueue := queues.NewQueue[any]()
				testQueue.Enqueue([]byte("invalid json"))
				w.queues.Register(testQueue)

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				// Process should handle invalid byte data gracefully
				w.processNextJob()
				time.Sleep(10 * time.Millisecond)

				assert.Equal(t, 0, testQueue.Len(), "Invalid byte data should be dequeued")
			})

			t.Run("processNextJob with invalid type", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				// Create invalid type data
				testQueue := queues.NewQueue[any]()
				testQueue.Enqueue(123) // Invalid type
				w.queues.Register(testQueue)

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				// Process should handle invalid type gracefully
				w.processNextJob()
				time.Sleep(10 * time.Millisecond)

				assert.Equal(t, 0, testQueue.Len(), "Invalid type should be dequeued")
			})

			t.Run("freePoolNode with worker stopping", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				}, WithConcurrency(1))

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				// Create many idle workers to trigger stopping
				for range 5 {
					node := w.initPoolNode()
					w.pool.PushNode(node)
				}

				// This should trigger the worker stopping branch in freePoolNode
				node := w.pool.PopBack()
				w.freePoolNode(node)

				// The node should be stopped and put back in cache
				assert.Greater(t, w.pool.Len(), 0, "Some workers should remain in pool")
			})

			t.Run("TunePool shrinking without idle expiry", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				}, WithConcurrency(5))

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				// Add workers to the pool
				for range 4 {
					node := w.initPoolNode()
					w.pool.PushNode(node)
				}

				initialPoolSize := w.pool.Len()

				// Shrink concurrency - should trigger pool shrinking
				err = w.TunePool(2)
				assert.NoError(t, err, "TunePool should not error")

				// Pool should be shrunk
				assert.Less(t, w.pool.Len(), initialPoolSize, "Pool should be shrunk")
			})

			t.Run("Pause when not running", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				err := w.Pause()
				assert.ErrorIs(t, err, ErrNotRunningWorker, "Pause should return error when worker not running")
			})

			t.Run("Stop when not running", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				err := w.Stop()
				assert.ErrorIs(t, err, ErrNotRunningWorker, "Stop should return error when worker not running")
			})

			t.Run("Resume from initiated state", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				assert.Equal(t, initiated, w.status.Load(), "Worker should be in initiated state")

				err := w.Resume()
				assert.NoError(t, err, "Resume should start worker from initiated state")
				assert.True(t, w.IsRunning(), "Worker should be running after Resume from initiated")

				w.Stop()
			})

			t.Run("Resume when already running", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				err = w.Resume()
				assert.ErrorIs(t, err, ErrRunningWorker, "Resume should return error when already running")
			})

			t.Run("Resume stopped worker", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")

				err = w.Stop()
				assert.NoError(t, err, "Worker should stop without error")

				err = w.Resume()
				assert.ErrorIs(t, err, ErrNotRunningWorker, "Resume should return error when worker is stopped")
			})

			t.Run("PauseAndWait with error", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				// PauseAndWait should return error when worker not running
				err := w.PauseAndWait()
				assert.ErrorIs(t, err, ErrNotRunningWorker, "PauseAndWait should return error when worker not running")
			})

			t.Run("processNextJob with empty queue", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				// Create an empty queue
				testQueue := queues.NewQueue[any]()
				w.queues.Register(testQueue)

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				// Process should handle empty queue gracefully
				w.processNextJob()
				time.Sleep(10 * time.Millisecond)
			})

			t.Run("processNextJob with byte parsing that creates wrong job type", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				})

				// Create a job with different type that would cause type assertion to fail
				job := newJob(123, loadJobConfigs(w.configs())) // int instead of string
				jobBytes, _ := job.Json()

				testQueue := queues.NewQueue[any]()
				testQueue.Enqueue(jobBytes)
				w.queues.Register(testQueue)

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				// Process should handle type mismatch gracefully
				w.processNextJob()
				time.Sleep(10 * time.Millisecond)
			})

			t.Run("TunePool edge case with empty pool", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				}, WithConcurrency(5))

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				// Clear the pool
				for w.pool.Len() > 0 {
					if node := w.pool.PopBack(); node != nil {
						node.Value.Stop()
					}
				}

				// Shrink concurrency with empty pool
				err = w.TunePool(2)
				assert.NoError(t, err, "TunePool should handle empty pool")
			})

			t.Run("goRemoveIdleWorkers edge cases", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {
					time.Sleep(5 * time.Millisecond)
				},
					WithConcurrency(10),
					WithIdleWorkerExpiryDuration(10*time.Millisecond),
					WithMinIdleWorkerRatio(50),
				)

				err := w.start()
				assert.NoError(t, err, "Worker should start without error")
				defer w.Stop()

				// Add nodes that are both in list and not in list to test edge cases
				for range 8 {
					node := w.initPoolNode()
					node.Value.UpdateLastUsed()
					w.pool.PushNode(node)
				}

				// Wait for idle worker removal to kick in
				time.Sleep(30 * time.Millisecond)

				// Should have cleaned up some workers
				assert.LessOrEqual(t, w.pool.Len(), w.numMinIdleWorkers()+1, "Pool should be cleaned up")
			})

			t.Run("processNextJob with parse failure", func(t *testing.T) {
				mockQueue := mocks.NewMockPersistentQueue()
				w := newWorker(func(j iJob[int]) {})

				mockQueue.Queue.Enqueue([]byte("invalid json"))

				queue := newQueue(w, mockQueue)
				w.queues.Register(queue.internalQueue)

				err := w.processNextJob()
				assert.Error(t, err)
				assert.ErrorIs(t, err, ErrParseJob, "should return ErrParseJob sentinel")
			})

			t.Run("processNextJob with cast failure after parse", func(t *testing.T) {
				mockQueue := mocks.NewMockPersistentQueue()
				// use newErrWorker so JobType is iErrorJob[string]
				w := newErrWorker(func(j iErrorJob[string]) {})

				// Create a valid JSON that parseToJob will accept and return *job[string]
				// *job[string] does NOT implement iErrorJob[string]
				jobData := []byte(`{"id":"123","payload":"test","status":"Created"}`)
				mockQueue.Queue.Enqueue(jobData)

				queue := newErrorQueue(w, mockQueue)
				w.queues.Register(queue.internalQueue)

				err := w.processNextJob()
				assert.Error(t, err)
				assert.Equal(t, ErrFailedToCastJob, err)
			})
		})

		// Group 8: Context-related tests
		t.Run("Context", func(t *testing.T) {
			t.Run("returns nil when no context configured", func(t *testing.T) {
				w := newWorker(func(j iJob[string]) {})
				assert.Nil(t, w.Context(), "Context should be nil when not configured")
			})

			t.Run("returns context when configured with WithContext", func(t *testing.T) {
				ctx := context.Background()
				w := newWorker(func(j iJob[string]) {}, WithContext(ctx))
				assert.NotNil(t, w.Context(), "Context should not be nil when configured")
			})

			t.Run("worker stops when context is cancelled", func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				w := newWorker(func(j iJob[string]) {
					time.Sleep(10 * time.Millisecond)
				}, WithContext(ctx))

				assert := assert.New(t)
				err := w.start()
				assert.NoError(err)
				assert.True(w.IsRunning())

				// Cancel the context
				cancel()

				// Wait for the worker to stop
				time.Sleep(50 * time.Millisecond)
				assert.True(w.IsStopped(), "Worker should be stopped after context cancellation")
			})

			t.Run("restart recreates context", func(t *testing.T) {
				ctx := t.Context()

				w := newWorker(func(j iJob[string]) {}, WithContext(ctx))

				assert := assert.New(t)
				err := w.start()
				assert.NoError(err)

				// Verify context exists
				assert.NotNil(w.Context())

				// Stop and restart cleanly to avoid race with goListenToContext
				err = w.Stop()
				assert.NoError(err)

				err = w.Restart()
				assert.NoError(err)

				// After restart, context should still exist and be valid
				newCtx := w.Context()
				assert.NotNil(newCtx, "Context should exist after restart")
				assert.NoError(newCtx.Err(), "New context should not be cancelled")

				w.Stop()
			})

			t.Run("newErrWorker with context", func(t *testing.T) {
				ctx := context.Background()
				w := newErrWorker(func(j iErrorJob[string]) {}, WithContext(ctx))
				assert.NotNil(t, w.Context(), "ErrWorker context should not be nil when configured")
			})

			t.Run("newResultWorker with context", func(t *testing.T) {
				ctx := context.Background()
				w := newResultWorker(func(j iResultJob[string, int]) {}, WithContext(ctx))
				assert.NotNil(t, w.Context(), "ResultWorker context should not be nil when configured")
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
				assert.Equal(idleWorkerExpiryDuration, w.Configs.idleWorkerExpiryDuration, "idle worker expiry duration should be set correctly")
				assert.Equal(minIdleWorkerRatio, w.Configs.minIdleWorkerRatio, "min idle worker ratio should be set correctly")

				expectedMinIdleWorkers := int(max((uint32(customConcurrency)*uint32(minIdleWorkerRatio))/100, 1))
				actualMinIdleWorkers := w.numMinIdleWorkers()
				assert.Equal(expectedMinIdleWorkers, actualMinIdleWorkers, "numMinIdleWorkers should return the expected value")
			})
		})
	})
}

func TestPublicWorkerErrors(t *testing.T) {
	t.Run("ErrGetNextQueue can be detected with errors.Is", func(t *testing.T) {
		w := newWorker(func(j iJob[string]) {})

		// Set an invalid strategy to trigger the error
		w.queues.strategy = Strategy(99) // Invalid strategy

		// Register a queue with some data to attempt processing
		mockQueue := mocks.NewMockPersistentQueue()
		mockQueue.Queue.Enqueue("test")
		w.queues.Register(mockQueue)

		err := w.processNextJob()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrGetNextQueue, "should return ErrGetNextQueue sentinel")
	})

	t.Run("ErrParseJob can be detected with errors.Is", func(t *testing.T) {
		mockQueue := mocks.NewMockPersistentQueue()
		w := newWorker(func(j iJob[int]) {})

		mockQueue.Queue.Enqueue([]byte("invalid json"))

		queue := newQueue(w, mockQueue)
		w.queues.Register(queue.internalQueue)

		err := w.processNextJob()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrParseJob, "should return ErrParseJob sentinel")
	})
}

type mockJob struct {
	*job[string]
	shouldFailClose bool
}

func (m *mockJob) Close() error {
	if m.shouldFailClose {
		return errors.New("close failure")
	}
	return m.job.Close()
}
