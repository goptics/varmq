package varmq

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goptics/varmq/internal/queues"
	"github.com/stretchr/testify/assert"
)

// TestWaiters comprehensively tests all wait methods and their interactions
// with worker lifecycle state transitions.
func TestWaiters(t *testing.T) {
	t.Run("Wait", func(t *testing.T) {
		t.Run("returns immediately when idle with no work", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			defer w.Stop()

			done := make(chan struct{})
			go func() {
				w.Wait()
				close(done)
			}()

			select {
			case <-done:
				// Success - returned immediately
			case <-time.After(100 * time.Millisecond):
				t.Fatal("Wait() should return immediately when idle with no work")
			}
		})

		t.Run("returns immediately when initiated", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			// Worker is in initiated state, never started

			done := make(chan struct{})
			go func() {
				w.Wait()
				close(done)
			}()

			select {
			case <-done:
				// Success
			case <-time.After(100 * time.Millisecond):
				t.Fatal("Wait() should return immediately for initiated worker")
			}
		})

		t.Run("waits for queue to drain", func(t *testing.T) {
			blockCh := make(chan struct{})
			started := make(chan struct{}, 2)
			w := newWorker(func(j iJob[string]) {
				started <- struct{}{}
				<-blockCh
			})
			assert := assert.New(t)

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)

			defer w.Stop()

			for i := range 2 {
				q.Enqueue(newJob("job-"+string(rune(i)), loadJobConfigs(w.configs())))
			}
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("Job should have started")
			}

			assert.Greater(w.NumProcessing(), 0, "At least one job should be in-flight")

			done := make(chan struct{})
			go func() {
				w.Wait()
				close(done)
			}()

			// Should not return immediately since queue has jobs
			select {
			case <-done:
				t.Fatal("Wait() should block while queue has pending jobs")
			case <-time.After(100 * time.Millisecond):
				// Expected - still waiting
			}

			// Release all jobs
			close(blockCh)

			// Wait for all jobs to process (generous timeout for -race)
			select {
			case <-done:
				// Success - all jobs processed
				assert.Equal(0, w.NumPending(), "Queue should be empty")
			case <-time.After(10 * time.Second):
				t.Fatal("Wait() should return after queue drains")
			}
		})

		t.Run("waits for in-flight jobs to complete", func(t *testing.T) {
			started := make(chan struct{})
			blockCh := make(chan struct{})
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh // Block until test releases
			})
			assert := assert.New(t)

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)

			defer w.Stop()

			// Enqueue a job that will block
			q.Enqueue(newJob("block-job", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			// Wait for job to start
			select {
			case <-started:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Job should have started")
			}

			assert.Equal(1, w.NumProcessing(), "Job should be in-flight")

			done := make(chan struct{})
			go func() {
				w.Wait()
				close(done)
			}()

			// Should not return while job is in-flight
			select {
			case <-done:
				t.Fatal("Wait() should block while jobs are in-flight")
			case <-time.After(100 * time.Millisecond):
				// Expected
			}

			// Release the blocked job
			close(blockCh)

			select {
			case <-done:
				// Success
				assert.Equal(0, w.NumProcessing(), "No jobs should be in-flight")
			case <-time.After(2 * time.Second):
				t.Fatal("Wait() should return after in-flight jobs complete")
			}
		})

		t.Run("returns when paused with no in-flight jobs", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			assert := assert.New(t)
			// Pause with no work
			err := w.Pause()
			assert.NoError(err)
			assert.True(w.IsPaused(), "Worker should be paused")

			done := make(chan struct{})
			go func() {
				w.Wait()
				close(done)
			}()

			select {
			case <-done:
				// Success - paused with no work means Wait returns
			case <-time.After(100 * time.Millisecond):
				t.Fatal("Wait() should return when paused with no in-flight jobs")
			}

			w.Stop()
		})

		t.Run("returns when stopped with no in-flight jobs", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			assert := assert.New(t)
			err := w.Stop()
			assert.NoError(err)
			w.WaitUntilStopped()
			assert.True(w.IsStopped(), "Worker should be stopped")

			done := make(chan struct{})
			go func() {
				w.Wait()
				close(done)
			}()

			select {
			case <-done:
				// Success
			case <-time.After(100 * time.Millisecond):
				t.Fatal("Wait() should return when stopped with no in-flight jobs")
			}
		})

		t.Run("waits during pausing transition", func(t *testing.T) {
			started := make(chan struct{})
			blockCh := make(chan struct{})
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh
			})
			assert := assert.New(t)

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			// Start a job that blocks
			q.Enqueue(newJob("block", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			// Wait for job to start
			select {
			case <-started:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Job should have started")
			}

			assert.Equal(1, w.NumProcessing(), "Job should be in-flight")

			// Pause while job is in-flight -> transitions to Pausing
			err := w.Pause()
			assert.NoError(err)
			assert.Equal(pausing, w.status.Load(), fmt.Sprintf("Should be pausing but Got: %s", w.Status()))

			done := make(chan struct{})
			go func() {
				w.Wait()
				close(done)
			}()

			// Should block while in Pausing with in-flight jobs
			select {
			case <-done:
				t.Fatal("Wait() should block during Pausing with in-flight jobs")
			case <-time.After(100 * time.Millisecond):
				// Expected
			}

			// Release the job
			close(blockCh)

			select {
			case <-done:
				// Success - after job completes, status is Paused, Wait returns
				assert.True(w.IsPaused(), "Worker should be paused")
			case <-time.After(2 * time.Second):
				t.Fatal("Wait() should return after pausing transition completes")
			}

			w.Stop()
		})
	})

	t.Run("WaitUntilIdle", func(t *testing.T) {
		t.Run("returns immediately when already idle", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			defer w.Stop()

			done := make(chan struct{})
			go func() {
				w.WaitUntilIdle()
				close(done)
			}()

			select {
			case <-done:
				// Success
			case <-time.After(100 * time.Millisecond):
				t.Fatal("WaitUntilIdle() should return immediately when already idle")
			}
		})

		t.Run("waits for running to idle transition", func(t *testing.T) {
			started := make(chan struct{})
			blockCh := make(chan struct{})
			w := newWorker(func(j iJob[string]) {
				close(started) // Signal that job has started
				<-blockCh      // Block until test releases
			})
			assert := assert.New(t)

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)

			defer w.Stop()

			// Add a job that will cause Running status
			q.Enqueue(newJob("job", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			// Wait for job to actually start processing
			select {
			case <-started:
				// Job has started
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Job should have started")
			}
			assert.True(w.IsRunning(), "Worker should be running")

			done := make(chan struct{})
			go func() {
				w.WaitUntilIdle()
				close(done)
			}()

			// Should block while Running
			select {
			case <-done:
				t.Fatal("WaitUntilIdle() should block while worker is Running")
			case <-time.After(50 * time.Millisecond):
				// Expected
			}

			// Release the job
			close(blockCh)

			// Wait for job to finish and transition to Idle
			select {
			case <-done:
				assert.True(w.IsIdle(), "Worker should be Idle after WaitUntilIdle returns")
			case <-time.After(200 * time.Millisecond):
				t.Fatal("WaitUntilIdle() should return after transition to Idle")
			}
		})

		t.Run("blocks when paused", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			assert := assert.New(t)
			err := w.Pause()
			assert.NoError(err)
			assert.True(w.IsPaused(), "Worker should be paused")

			done := make(chan struct{})
			go func() {
				w.WaitUntilIdle()
				close(done)
			}()

			select {
			case <-done:
				t.Fatal("WaitUntilIdle() should block when worker is Paused")
			case <-time.After(100 * time.Millisecond):
			}

			w.Resume()
			<-done

			w.Stop()
		})

		t.Run("blocks when stopped", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			assert := assert.New(t)
			err := w.Stop()
			assert.NoError(err)
			w.WaitUntilStopped()
			assert.True(w.IsStopped(), "Worker should be stopped")

			done := make(chan struct{})
			go func() {
				w.WaitUntilIdle()
				close(done)
			}()

			select {
			case <-done:
				t.Fatal("WaitUntilIdle() should block when worker is Stopped")
			case <-time.After(100 * time.Millisecond):
			}

			w.Restart()
			<-done

			w.Stop()
		})
	})

	t.Run("WaitUntilPaused", func(t *testing.T) {
		t.Run("returns immediately when already paused", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			assert := assert.New(t)
			err := w.Pause()
			assert.NoError(err)
			assert.True(w.IsPaused(), "Worker should be paused")

			done := make(chan struct{})
			go func() {
				w.WaitUntilPaused()
				close(done)
			}()

			select {
			case <-done:
				// Success
			case <-time.After(100 * time.Millisecond):
				t.Fatal("WaitUntilPaused() should return immediately when already paused")
			}

			w.Stop()
		})

		t.Run("waits for pausing to paused transition", func(t *testing.T) {
			blockCh := make(chan struct{})
			started := make(chan struct{})
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh
			})
			assert := assert.New(t)

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("block", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("Job should have started")
			}

			assert.Equal(1, w.NumProcessing(), "Job should be in-flight")

			// Pause -> transitions to Pausing
			err := w.Pause()
			assert.NoError(err)
			assert.Equal(pausing, w.status.Load(), fmt.Sprintf("Should be pausing but Got: %s", w.Status()))

			done := make(chan struct{})
			go func() {
				w.WaitUntilPaused()
				close(done)
			}()

			// Should block while Pausing
			select {
			case <-done:
				t.Fatal("WaitUntilPaused() should block while status is Pausing")
			case <-time.After(100 * time.Millisecond):
				// Expected
			}

			// Release the job
			close(blockCh)

			select {
			case <-done:
				assert.True(w.IsPaused(), "Worker should be Paused")
			case <-time.After(2 * time.Second):
				t.Fatal("WaitUntilPaused() should return after transition to Paused")
			}

			w.Stop()
		})

		t.Run("blocks when never paused", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})

			done := make(chan struct{})
			go func() {
				w.WaitUntilPaused()
				close(done)
			}()

			select {
			case <-done:
				t.Fatal("WaitUntilPaused() should block when worker is never paused")
			case <-time.After(100 * time.Millisecond):
			}

			w.Pause()
			<-done

			w.Stop()
		})
	})

	t.Run("WaitUntilStopped", func(t *testing.T) {
		t.Run("returns immediately when already stopped", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			assert := assert.New(t)
			err := w.Stop()
			assert.NoError(err)
			w.WaitUntilStopped()
			assert.True(w.IsStopped(), "Worker should be stopped")

			done := make(chan struct{})
			go func() {
				w.WaitUntilStopped()
				close(done)
			}()

			select {
			case <-done:
				// Success
			case <-time.After(100 * time.Millisecond):
				t.Fatal("WaitUntilStopped() should return immediately when already stopped")
			}
		})

		t.Run("waits for stopping to stopped transition", func(t *testing.T) {
			blockCh := make(chan struct{})
			started := make(chan struct{})
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh
			})
			assert := assert.New(t)

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("block", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("Job should have started")
			}

			assert.Equal(1, w.NumProcessing(), "Job should be in-flight")

			// Stop -> transitions to Stopping
			err := w.Stop()
			assert.NoError(err)
			assert.Equal(stopping, w.status.Load(), "Status should be Stopping")

			done := make(chan struct{})
			go func() {
				w.WaitUntilStopped()
				close(done)
			}()

			// Should block while Stopping
			select {
			case <-done:
				t.Fatal("WaitUntilStopped() should block while status is Stopping")
			case <-time.After(50 * time.Millisecond):
				// Expected
			}

			// Release the job
			close(blockCh)

			select {
			case <-done:
				assert.True(w.IsStopped(), "Worker should be Stopped")
			case <-time.After(200 * time.Millisecond):
				t.Fatal("WaitUntilStopped() should return after transition to Stopped")
			}
		})

		t.Run("blocks when never stopped", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			defer w.Stop()

			done := make(chan struct{})
			go func() {
				w.WaitUntilStopped()
				close(done)
			}()

			// Worker is Idle, never stopped - should block indefinitely
			select {
			case <-done:
				t.Fatal("WaitUntilStopped() should block when worker is never stopped")
			case <-time.After(100 * time.Millisecond):
				// Expected - still blocked
			}
		})
	})

	t.Run("PauseAndWait", func(t *testing.T) {
		t.Run("with in-flight jobs blocks until paused", func(t *testing.T) {
			started := make(chan struct{})
			blockCh := make(chan struct{})
			var processed bool
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh
				processed = true
			})
			assert := assert.New(t)

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			// Start a blocking job
			q.Enqueue(newJob("block", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("Job should have started")
			}

			assert.Equal(1, w.NumProcessing(), "Job should be in-flight")

			done := make(chan struct{})
			go func() {
				err := w.PauseAndWait()
				assert.NoError(err)
				close(done)
			}()

			// Should block while job is in-flight
			select {
			case <-done:
				t.Fatal("PauseAndWait() should block while jobs are in-flight")
			case <-time.After(50 * time.Millisecond):
				// Expected
			}

			// Release the job
			close(blockCh)

			select {
			case <-done:
				assert.True(w.IsPaused(), "Worker should be Paused")
				assert.True(processed, "Job should have completed")
				assert.Equal(0, w.NumProcessing(), "No jobs should be in-flight")
			case <-time.After(200 * time.Millisecond):
				t.Fatal("PauseAndWait() should return after all jobs complete")
			}

			w.Stop()
		})

		t.Run("with no jobs returns immediately paused", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			assert := assert.New(t)
			err := w.PauseAndWait()
			assert.NoError(err)
			assert.True(w.IsPaused(), "Worker should be paused immediately")

			w.Stop()
		})

		t.Run("returns error when not running", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {}, WithAutoRun(false))

			err := w.PauseAndWait()
			assert.ErrorIs(t, err, ErrNotRunningWorker)
		})
	})

	t.Run("WaitAndPause", func(t *testing.T) {
		t.Run("waits for work then pauses", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {
				time.Sleep(500 * time.Millisecond)
			}, WithAutoRun(false))
			assert := assert.New(t)

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("job", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			w.Start()
			defer w.Stop()

			done := make(chan struct{})
			go func() {
				err := w.WaitAndPause()
				assert.NoError(err)
				close(done)
			}()

			select {
			case <-done:
				t.Fatal("WaitAndPause() should block while work is in progress")
			case <-time.After(200 * time.Millisecond):
			}

			select {
			case <-done:
				assert.True(w.IsPaused(), "Worker should be Paused")
				assert.Equal(0, w.NumPending(), "Queue should be empty")
			case <-time.After(2 * time.Second):
				t.Fatal("WaitAndPause() should return after work completes")
			}

		})

		t.Run("returns immediately when no work", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			assert := assert.New(t)
			err := w.WaitAndPause()
			assert.NoError(err)
			assert.True(w.IsPaused(), "Worker should be paused")

			w.Stop()
		})
	})

	t.Run("StopAndWait", func(t *testing.T) {
		t.Run("with in-flight jobs blocks until stopped", func(t *testing.T) {
			blockCh := make(chan struct{})
			started := make(chan struct{})
			var processed bool
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh
				processed = true
			}, WithAutoRun(false))
			assert := assert.New(t)

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("block", loadJobConfigs(w.configs())))
			w.Start()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("Job should have started")
			}

			assert.Equal(1, w.NumProcessing(), "Job should be in-flight")

			done := make(chan struct{})
			go func() {
				err := w.StopAndWait()
				assert.NoError(err)
				close(done)
			}()

			// Should block while job is in-flight
			select {
			case <-done:
				t.Fatal("StopAndWait() should block while jobs are in-flight")
			case <-time.After(50 * time.Millisecond):
				// Expected
			}

			// Release the job
			close(blockCh)

			select {
			case <-done:
				assert.True(w.IsStopped(), "Worker should be Stopped")
				assert.True(processed, "Job should have completed")
				assert.Equal(0, w.NumProcessing(), "No jobs should be in-flight")
			case <-time.After(200 * time.Millisecond):
				t.Fatal("StopAndWait() should return after all jobs complete")
			}

			w.Stop()
		})

		t.Run("with no jobs returns immediately stopped", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			assert := assert.New(t)
			err := w.StopAndWait()
			assert.NoError(err)
			assert.True(w.IsStopped(), "Worker should be stopped immediately")
		})
	})

	t.Run("WaitAndStop", func(t *testing.T) {
		t.Run("waits for work then stops", func(t *testing.T) {
			blockCh := make(chan struct{})
			started := make(chan struct{})
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh
			})
			assert := assert.New(t)

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("job", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("Job should have started")
			}

			assert.Equal(1, w.NumProcessing(), "Job should be in-flight")

			done := make(chan struct{})
			go func() {
				err := w.WaitAndStop()
				assert.NoError(err)
				close(done)
			}()

			// Should block while job is processing
			select {
			case <-done:
				t.Fatal("WaitAndStop() should block while work is in progress")
			case <-time.After(50 * time.Millisecond):
				// Expected
			}

			// Release the job
			close(blockCh)

			// Wait for job to complete and stop
			select {
			case <-done:
				assert.True(w.IsStopped(), "Worker should be Stopped")
				assert.Equal(0, w.NumPending(), "Queue should be empty")
			case <-time.After(2 * time.Second):
				t.Fatal("WaitAndStop() should return after work completes")
			}

			w.Stop()
		})

		t.Run("returns immediately when no work", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			assert := assert.New(t)
			err := w.WaitAndStop()
			assert.NoError(err)
			assert.True(w.IsStopped(), "Worker should be stopped")
		})
	})

	t.Run("Wait vs WaitUntilIdle distinction", func(t *testing.T) {
		t.Run("Wait returns when paused, WaitUntilIdle blocks", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			assert := assert.New(t)
			err := w.Pause()
			assert.NoError(err)
			assert.True(w.IsPaused(), "Worker should be paused")

			doneWait := make(chan struct{})
			go func() {
				w.Wait()
				close(doneWait)
			}()

			select {
			case <-doneWait:
			case <-time.After(100 * time.Millisecond):
				t.Fatal("Wait() should return when paused with no work")
			}

			doneIdle := make(chan struct{})
			go func() {
				w.WaitUntilIdle()
				close(doneIdle)
			}()

			select {
			case <-doneIdle:
				t.Fatal("WaitUntilIdle() should block when status is Paused")
			case <-time.After(100 * time.Millisecond):
			}

			w.Resume()
			<-doneIdle

			w.Stop()
		})
	})

	t.Run("Restart uses Wait correctly", func(t *testing.T) {
		t.Run("restart with running jobs waits then restarts", func(t *testing.T) {
			started := make(chan struct{})
			var startOnce sync.Once
			blockCh := make(chan struct{})
			var processed atomic.Int32
			w := newWorker(func(j iJob[int]) {
				processed.Add(1)
				startOnce.Do(func() { close(started) })
				<-blockCh
			})
			assert := assert.New(t)

			q := queues.NewQueue[iJob[int]]()
			w.queues.Register(q, 1)
			// Start some jobs
			for i := range 3 {
				q.Enqueue(newJob(i, loadJobConfigs(w.configs())))
			}
			w.notifyToPullNextJobs()

			// Wait for at least one job to start
			select {
			case <-started:
				// Job started
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Some jobs should have started")
			}
			assert.Greater(processed.Load(), int32(0), "Some jobs should have started")

			// Restart should wait for in-flight jobs, then restart
			done := make(chan struct{})
			go func() {
				err := w.Restart()
				assert.NoError(err)
				close(done)
			}()

			// Should block while jobs are in-flight
			select {
			case <-done:
				t.Fatal("Restart() should block while jobs are in-flight")
			case <-time.After(50 * time.Millisecond):
				// Expected
			}

			// Release all jobs
			close(blockCh)

			select {
			case <-done:
				assert.True(w.IsActive(), "Worker should be active after restart")
			case <-time.After(2 * time.Second):
				t.Fatal("Restart() should return after in-flight jobs complete")
			}

			w.Stop()
		})
	})
}
