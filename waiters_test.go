package varmq

import (
	"fmt"
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
			w.Wait()
		})

		t.Run("returns immediately when initiated", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {}, WithAutoRun(false))
			w.Wait()
		})

		t.Run("waits for queue to drain", func(t *testing.T) {
			blockCh := make(chan struct{})
			started := make(chan struct{}, 2)
			w := newWorker(func(j iJob[string]) {
				started <- struct{}{}
				<-blockCh
			})

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

			assert.Greater(t, w.NumProcessing(), 0, "At least one job should be in-flight")

			done := make(chan struct{})
			go func() {
				w.Wait()
				close(done)
			}()

			close(blockCh)

			select {
			case <-done:
				assert.Equal(t, 0, w.NumPending(), "Queue should be empty")
			case <-time.After(10 * time.Second):
				t.Fatalf("Wait() should return after queue drains, status=%s", w.Status())
			}
		})

		t.Run("waits for in-flight jobs to complete", func(t *testing.T) {
			started := make(chan struct{})
			blockCh := make(chan struct{})
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh
			})

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)

			defer w.Stop()

			q.Enqueue(newJob("block-job", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Job should have started")
			}

			assert.Equal(t, 1, w.NumProcessing(), "Job should be in-flight")

			done := make(chan struct{})
			go func() {
				w.Wait()
				close(done)
			}()

			close(blockCh)

			select {
			case <-done:
				assert.Equal(t, 0, w.NumProcessing(), "No jobs should be in-flight")
			case <-time.After(2 * time.Second):
				t.Fatalf("Wait() should return after in-flight jobs complete, status=%s", w.Status())
			}
		})

		t.Run("returns when paused with no in-flight jobs", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			err := w.Pause()
			assert.NoError(t, err)
			assert.True(t, w.IsPaused(), "Worker should be paused")

			w.Wait()

			w.Stop()
		})

		t.Run("returns when stopped with no in-flight jobs", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			err := w.Stop()
			assert.NoError(t, err)
			w.WaitUntilStopped()
			assert.True(t, w.IsStopped(), "Worker should be stopped")

			w.Wait()
		})

		t.Run("waits during pausing transition", func(t *testing.T) {
			started := make(chan struct{})
			blockCh := make(chan struct{})
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh
			})

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("block", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Job should have started")
			}

			assert.Equal(t, 1, w.NumProcessing(), "Job should be in-flight")

			err := w.Pause()
			assert.NoError(t, err)
			assert.Equal(t, pausing, w.status.Load(), fmt.Sprintf("Should be pausing but Got: %s", w.Status()))

			done := make(chan struct{})
			go func() {
				w.Wait()
				close(done)
			}()

			close(blockCh)

			select {
			case <-done:
				assert.True(t, w.IsPaused(), "Worker should be paused")
			case <-time.After(2 * time.Second):
				t.Fatalf("Wait() should return after pausing transition completes, status=%s", w.Status())
			}

			w.Stop()
		})
	})

	t.Run("WaitUntilIdle", func(t *testing.T) {
		t.Run("returns immediately when already idle", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			defer w.Stop()
			w.WaitUntilIdle()
		})

		t.Run("waits for running to idle transition", func(t *testing.T) {
			started := make(chan struct{})
			blockCh := make(chan struct{})
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh
			})

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)

			defer w.Stop()

			q.Enqueue(newJob("job", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Job should have started")
			}
			assert.True(t, w.IsRunning(), "Worker should be running")

			close(blockCh)

			assert.Eventually(t, w.IsIdle, 2*time.Second, 10*time.Millisecond, "WaitUntilIdle() should return after transition to Idle")
		})

		t.Run("blocks when paused", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			err := w.Pause()
			assert.NoError(t, err)
			assert.True(t, w.IsPaused(), "Worker should be paused")

			done := make(chan struct{})
			go func() {
				w.WaitUntilIdle()
				close(done)
			}()

			select {
			case <-done:
				t.Fatalf("WaitUntilIdle() should block when worker is Paused, status=%s", w.Status())
			case <-time.After(100 * time.Millisecond):
			}

			w.Resume()
			<-done

			w.Stop()
		})

		t.Run("blocks when stopped", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			err := w.Stop()
			assert.NoError(t, err)
			w.WaitUntilStopped()
			assert.True(t, w.IsStopped(), "Worker should be stopped")

			done := make(chan struct{})
			go func() {
				w.WaitUntilIdle()
				close(done)
			}()

			select {
			case <-done:
				t.Fatalf("WaitUntilIdle() should block when worker is Stopped, status=%s", w.Status())
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
			err := w.Pause()
			assert.NoError(t, err)
			assert.True(t, w.IsPaused(), "Worker should be paused")

			w.WaitUntilPaused()

			w.Stop()
		})

		t.Run("waits for pausing to paused transition", func(t *testing.T) {
			blockCh := make(chan struct{})
			started := make(chan struct{})
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh
			})

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("block", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("Job should have started")
			}

			assert.Equal(t, 1, w.NumProcessing(), "Job should be in-flight")

			err := w.Pause()
			assert.NoError(t, err)
			assert.Equal(t, pausing, w.status.Load(), fmt.Sprintf("Should be pausing but Got: %s", w.Status()))

			close(blockCh)

			assert.Eventually(t, w.IsPaused, 2*time.Second, 10*time.Millisecond, "WaitUntilPaused() should return after transition to Paused")

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
				t.Fatalf("WaitUntilPaused() should block when worker is never paused, status=%s", w.Status())
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
			err := w.Stop()
			assert.NoError(t, err)
			w.WaitUntilStopped()
			assert.True(t, w.IsStopped(), "Worker should be stopped")
		})

		t.Run("waits for stopping to stopped transition", func(t *testing.T) {
			blockCh := make(chan struct{})
			started := make(chan struct{})
			w := newWorker(func(j iJob[string]) {
				close(started)
				<-blockCh
			})

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("block", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("Job should have started")
			}

			assert.Equal(t, 1, w.NumProcessing(), "Job should be in-flight")

			err := w.Stop()
			assert.NoError(t, err)
			assert.Equal(t, stopping, w.status.Load(), "Status should be Stopping")

			close(blockCh)

			assert.Eventually(t, w.IsStopped, 2*time.Second, 10*time.Millisecond, "WaitUntilStopped() should return after transition to Stopped")
		})

		t.Run("blocks when never stopped", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			defer w.Stop()

			done := make(chan struct{})
			go func() {
				w.WaitUntilStopped()
				close(done)
			}()

			select {
			case <-done:
				t.Fatalf("WaitUntilStopped() should block when worker is never stopped, status=%s", w.Status())
			case <-time.After(100 * time.Millisecond):
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

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("block", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("Job should have started")
			}

			assert.Equal(t, 1, w.NumProcessing(), "Job should be in-flight")

			done := make(chan struct{})
			go func() {
				assert.NoError(t, w.PauseAndWait())
				close(done)
			}()

			close(blockCh)

			select {
			case <-done:
				assert.True(t, w.IsPaused(), "Worker should be Paused")
				assert.True(t, processed, "Job should have completed")
				assert.Equal(t, 0, w.NumProcessing(), "No jobs should be in-flight")
			case <-time.After(2 * time.Second):
				t.Fatalf("PauseAndWait() should return after all jobs complete, status=%s", w.Status())
			}

			w.Stop()
		})

		t.Run("with no jobs returns immediately paused", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			err := w.PauseAndWait()
			assert.NoError(t, err)
			assert.True(t, w.IsPaused(), "Worker should be paused immediately")

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
				time.Sleep(100 * time.Millisecond)
			}, WithAutoRun(false))

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("job", loadJobConfigs(w.configs())))
			w.Start()
			defer w.Stop()

			assert.NoError(t, w.WaitAndPause())
			assert.True(t, w.IsPaused(), "Worker should be Paused")
			assert.Equal(t, 0, w.NumPending(), "Queue should be empty")
		})

		t.Run("returns immediately when no work", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			err := w.WaitAndPause()
			assert.NoError(t, err)
			assert.True(t, w.IsPaused(), "Worker should be paused")

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

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("block", loadJobConfigs(w.configs())))
			w.Start()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("Job should have started")
			}

			assert.Equal(t, 1, w.NumProcessing(), "Job should be in-flight")

			done := make(chan struct{})
			go func() {
				assert.NoError(t, w.StopAndWait())
				close(done)
			}()

			close(blockCh)

			select {
			case <-done:
				assert.True(t, w.IsStopped(), "Worker should be Stopped")
				assert.True(t, processed, "Job should have completed")
				assert.Equal(t, 0, w.NumProcessing(), "No jobs should be in-flight")
			case <-time.After(2 * time.Second):
				t.Fatalf("StopAndWait() should return after all jobs complete, status=%s", w.Status())
			}

			w.Stop()
		})

		t.Run("with no jobs returns immediately stopped", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			err := w.StopAndWait()
			assert.NoError(t, err)
			assert.True(t, w.IsStopped(), "Worker should be stopped immediately")
		})

		t.Run("returns error when Stop fails", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {}, WithAutoRun(false))
			err := w.StopAndWait()
			assert.ErrorIs(t, err, ErrNotRunningWorker)
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

			q := queues.NewQueue[iJob[string]]()
			w.queues.Register(q, 1)
			q.Enqueue(newJob("job", loadJobConfigs(w.configs())))
			w.notifyToPullNextJobs()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("Job should have started")
			}

			assert.Equal(t, 1, w.NumProcessing(), "Job should be in-flight")

			done := make(chan struct{})
			go func() {
				assert.NoError(t, w.WaitAndStop())
				close(done)
			}()

			close(blockCh)

			select {
			case <-done:
				w.WaitUntilStopped()
				assert.True(t, w.IsStopped(), "Worker should be Stopped")
				assert.Equal(t, 0, w.NumPending(), "Queue should be empty")
			case <-time.After(2 * time.Second):
				t.Fatalf("WaitAndStop() should return after work completes, status=%s", w.Status())
			}

			w.Stop()
		})

		t.Run("returns immediately when no work", func(t *testing.T) {
			w := newWorker(func(j iJob[string]) {})
			err := w.WaitAndStop()
			assert.NoError(t, err)
			w.WaitUntilStopped()
			assert.True(t, w.IsStopped(), "Worker should be stopped")
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

	t.Run("Restart blocks until in-flight jobs drain", func(t *testing.T) {
		blockCh := make(chan struct{})
		w := newWorker(func(j iJob[int]) {
			<-blockCh
		}, WithAutoRun(false))
		q := queues.NewQueue[iJob[int]]()
		w.queues.Register(q, 1)
		q.Enqueue(newJob(1, loadJobConfigs(w.configs())))
		w.Start()

		assert.Eventually(t, func() bool { return w.NumProcessing() > 0 }, 200*time.Millisecond, 5*time.Millisecond)

		done := make(chan struct{})
		go func() {
			assert.NoError(t, w.Restart())
			close(done)
		}()

		close(blockCh)

		select {
		case <-done:
			assert.True(t, w.IsActive(), "Worker should be active after restart")
		case <-time.After(2 * time.Second):
			t.Fatal("Restart() should return after in-flight jobs complete")
		}

		w.Stop()
	})
}
