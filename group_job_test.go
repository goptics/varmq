package varmq

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type groupAcknowledgementQueue struct {
	*mocks.MockPersistentQueue
	acknowledged chan string
	failures     []bool
	next         atomic.Uint32
}

func (q *groupAcknowledgementQueue) Acknowledge(id string) bool {
	q.acknowledged <- id
	return !q.failures[q.next.Add(1)-1]
}

func TestGroupJobAcknowledgements(t *testing.T) {
	tests := []struct {
		name string
		bind func(IQueue, []Item[string], chan<- StatusProvider) (Worker, EnqueuedGroupJob)
	}{
		{
			name: "plain worker",
			bind: func(q IQueue, items []Item[string], processed chan<- StatusProvider) (Worker, EnqueuedGroupJob) {
				w := NewWorker(func(j Job[string]) {
					processed <- j.(StatusProvider)
				}, WithAutoRun(false), WithConcurrency(1))
				return w, w.WithQueue(q).AddAll(items)
			},
		},
		{
			name: "error worker",
			bind: func(q IQueue, items []Item[string], processed chan<- StatusProvider) (Worker, EnqueuedGroupJob) {
				w := NewErrWorker(func(j Job[string]) error {
					processed <- j.(StatusProvider)
					return nil
				}, WithAutoRun(false), WithConcurrency(1))
				return w, w.WithQueue(q).AddAll(items)
			},
		},
		{
			name: "result worker",
			bind: func(q IQueue, items []Item[string], processed chan<- StatusProvider) (Worker, EnqueuedGroupJob) {
				w := NewResultWorker(func(j Job[string]) (int, error) {
					processed <- j.(StatusProvider)
					return len(j.Data()), nil
				}, WithAutoRun(false), WithConcurrency(1))
				return w, w.WithQueue(q).AddAll(items)
			},
		},
	}

	for _, tt := range tests {
		for _, outcome := range []struct {
			name     string
			failures []bool
		}{
			{"success", []bool{false}},
			{"failure", []bool{true}},
			{"rejection then success", []bool{true, false}},
			{"success then rejection", []bool{false, true}},
		} {
			t.Run(tt.name+"/"+outcome.name, func(t *testing.T) {
				items := []Item[string]{{ID: "test-job", Data: "payload"}}
				if len(outcome.failures) == 2 {
					items = append(items, Item[string]{ID: "next-job", Data: "next payload"})
				}
				q := &groupAcknowledgementQueue{
					MockPersistentQueue: mocks.NewMockPersistentQueue(),
					acknowledged:        make(chan string, len(items)+1),
					failures:            outcome.failures,
				}
				processed := make(chan StatusProvider, len(items))
				w, group := tt.bind(q, items, processed)
				require.NoError(t, w.Start())
				t.Cleanup(func() { assert.NoError(t, w.StopAndWait()) })
				w.Wait()

				for range items {
					select {
					case id := <-q.acknowledged:
						assert.Equal(t, "mock-ack-id", id)
					default:
						t.Error("group job completion did not acknowledge the queue")
					}
				}
				select {
				case id := <-q.acknowledged:
					t.Errorf("group job was acknowledged twice: %s", id)
				default:
				}

				for range items {
					select {
					case job := <-processed:
						assert.Equal(t, "Closed", job.Status())
						assert.True(t, job.IsClosed())
					default:
						t.Error("group job was not processed")
					}
				}

				require.Zero(t, group.NumPending())
				group.Wait()
				for i, fail := range outcome.failures {
					if !fail {
						continue
					}
					select {
					case err := <-w.Errs():
						assert.ErrorIs(t, err, ErrAcknowledgeJob)
						assert.Contains(t, err.Error(), "g:"+items[i].ID)
						assert.Contains(t, err.Error(), "mock-ack-id")
					default:
						t.Error("rejected acknowledgement did not reach the worker error channel")
					}
				}
				select {
				case err := <-w.Errs():
					t.Errorf("unexpected worker error: %v", err)
				default:
				}

				switch group := group.(type) {
				case EnqueuedErrGroupJob:
					assertGroupResponseChannelClosed(t, group.Errs())
				case EnqueuedResultGroupJob[int]:
					for _, item := range items {
						select {
						case result, ok := <-group.Results():
							require.True(t, ok)
							assert.Equal(t, "g:"+item.ID, result.JobId)
							assert.Equal(t, len(item.Data), result.Data)
							assert.NoError(t, result.Err)
						default:
							t.Fatal("group job did not return its result")
						}
					}
					assertGroupResponseChannelClosed(t, group.Results())
				}
			})
		}
	}
}

func assertGroupResponseChannelClosed[T any](t *testing.T, responses <-chan T) {
	t.Helper()
	select {
	case _, ok := <-responses:
		require.False(t, ok, "group response channel contained another response")
	default:
		t.Error("group response channel remained open after job completion")
	}
}

func TestGroupJob(t *testing.T) {
	t.Run("group job creation with newGroupJob", func(t *testing.T) {
		// Create a new group job with buffer size 3
		bufferSize := 3
		gj := newGroupJob[string](bufferSize)

		assert := assert.New(t)

		// Validate group job structure
		assert.NotNil(gj, "group job should not be nil")
		assert.NotNil(gj.wgc, "WaitGroup counter should be initialized")
		assert.Equal(bufferSize, gj.NumPending(), "initial pending count should match buffer size")
	})

	t.Run("generate group ID", func(t *testing.T) {
		// Test group ID generation
		baseId := "test-job"
		groupId := generateGroupId(baseId)

		assert := assert.New(t)

		// Group ID should have the expected prefix
		assert.True(strings.HasPrefix(groupId, groupIdPrefixed), "group ID should have the correct prefix")
		assert.Equal(groupIdPrefixed+baseId, groupId, "group ID should be prefix + base ID")
	})

	t.Run("creating new job in a group", func(t *testing.T) {
		// Create a new group job
		bufferSize := 3
		gj := newGroupJob[string](bufferSize)

		// Create a job within the group
		jobData := "test group data"
		jobId := "group-job-123"
		newJob := gj.newJob(jobData, jobConfigs{Id: jobId})

		assert := assert.New(t)

		// Validate the new job
		assert.NotNil(newJob, "new job in group should not be nil")
		assert.Equal(generateGroupId(jobId), newJob.ID(), "job ID should have group prefix")
		assert.Equal(jobData, newJob.Data(), "job data should match")
		assert.Equal(gj.wgc, newJob.wgc, "WaitGroup counter should be shared with the group")
	})

	t.Run("job status transitions in a group job", func(t *testing.T) {
		// Create a new group job
		bufferSize := 1
		gj := newGroupJob[string](bufferSize)

		assert := assert.New(t)

		// Initial status should be default (0 value, which is 'created')
		assert.Equal("Created", gj.Status(), "initial status should be 'Created'")

		// Transition to queued
		gj.changeStatus(queued)
		assert.Equal("Queued", gj.Status(), "status should be 'Queued' after change")

		// Transition to processing
		gj.changeStatus(processing)
		assert.Equal("Processing", gj.Status(), "status should be 'Processing' after change")

		// Transition to finished
		gj.changeStatus(finished)
		assert.Equal("Finished", gj.Status(), "status should be 'Finished' after change")
	})

	t.Run("waiting for group job completion", func(t *testing.T) {
		// Create a group job
		bufferSize := 1
		gj := newGroupJob[string](bufferSize)

		assert := assert.New(t)

		// Check the initial pending count
		assert.Equal(bufferSize, gj.NumPending(), "initial pending count should match buffer size")

		// Test Wait method in a goroutine
		done := make(chan struct{})
		go func() {
			defer close(done)
			gj.Wait() // This should block until all jobs are done
		}()

		// Try to receive - this should timeout because Wait is blocking
		select {
		case <-done:
			assert.Fail("Wait returned before jobs were completed")
		case <-time.After(100 * time.Millisecond):
			// This is expected - Wait is still blocking
		}

		// Complete all pending jobs
		for range bufferSize {
			gj.wgc.Done()
		}

		// Now should have 0 pending jobs
		assert.Equal(0, gj.NumPending(), "pending count should be 0 after completion")

		// Wait should now complete
		select {
		case <-done:
			// This is expected - Wait has completed
		case <-time.After(100 * time.Millisecond):
			assert.Fail("Wait did not return after jobs were completed")
		}
	})

	t.Run("closing a group job", func(t *testing.T) {
		// Create a group job
		bufferSize := 2
		gj := newGroupJob[string](bufferSize)

		// Set job status to make it closeable
		gj.changeStatus(created)

		assert := assert.New(t)

		// Close the job
		err := gj.Close()
		assert.Nil(err, "closing group job should not fail")
		assert.Equal("Closed", gj.Status(), "job status should be 'Closed' after close")
		assert.True(gj.IsClosed(), "job should be marked as closed")
	})

	t.Run("closing a processing group job should fail", func(t *testing.T) {
		// Create a group job
		bufferSize := 2
		gj := newGroupJob[string](bufferSize)

		// Set job status to processing
		gj.changeStatus(processing)

		assert := assert.New(t)

		// Try to close the job - should fail with processing error
		err := gj.Close()
		expectedError := errors.New("job is processing, you can't close processing job")
		assert.NotNil(err, "closing a processing job should fail")
		assert.Equal(expectedError.Error(), err.Error(), "error message should indicate job is processing")
	})

	t.Run("closing an already closed group job should fail", func(t *testing.T) {
		// Create a group job
		bufferSize := 2
		gj := newGroupJob[string](bufferSize)

		// Set job status to closed
		gj.changeStatus(closed)

		assert := assert.New(t)

		// Try to close the job - should fail with already closed error
		err := gj.Close()
		expectedError := errors.New("job is already closed")
		assert.NotNil(err, "closing an already closed job should fail")
		assert.Equal(expectedError.Error(), err.Error(), "error message should indicate job is already closed")
	})
}

func TestResultGroupJob(t *testing.T) {
	t.Run("result group job creation", func(t *testing.T) {
		// Create a new result group job with buffer size 3
		bufferSize := 3
		rgj := newResultGroupJob[string, int](bufferSize)

		assert := assert.New(t)

		// Validate result group job structure
		assert.NotNil(rgj, "result group job should not be nil")
		assert.NotNil(rgj.wgc, "WaitGroup counter should be initialized")
		assert.Equal(bufferSize, rgj.NumPending(), "initial pending count should match buffer size")
	})

	t.Run("result group job Wait method", func(t *testing.T) {
		// Create a result group job
		bufferSize := 1
		rgj := newResultGroupJob[string, int](bufferSize)

		assert := assert.New(t)

		// Test Wait method in a goroutine
		done := make(chan struct{})
		go func() {
			defer close(done)
			rgj.Wait() // This should block until all jobs are done
		}()

		// Try to receive - this should timeout because Wait is blocking
		select {
		case <-done:
			assert.Fail("Wait returned before jobs were completed")
		case <-time.After(100 * time.Millisecond):
			// This is expected - Wait is still blocking
		}

		// Complete all pending jobs
		for range bufferSize {
			rgj.wgc.Done()
		}

		// Wait should now complete
		select {
		case <-done:
			// This is expected - Wait has completed
		case <-time.After(100 * time.Millisecond):
			assert.Fail("Wait did not return after jobs were completed")
		}
	})

	// We're skipping the "result group job Results error branch" test because
	// proper error simulation would require a complete reimplementation of
	// the channel behavior, which is outside the scope of these tests

	t.Run("creating new job in a result group", func(t *testing.T) {
		// Create a new result group job
		bufferSize := 3
		rgj := newResultGroupJob[string, int](bufferSize)

		// Create a job within the group
		jobData := "test result group data"
		jobId := "result-group-job-123"
		newJob := rgj.newJob(jobData, jobConfigs{Id: jobId})

		assert := assert.New(t)

		// Validate the new job
		assert.NotNil(newJob, "new job in result group should not be nil")
		assert.Equal(generateGroupId(jobId), newJob.ID(), "job ID should have group prefix")
		assert.Equal(jobData, newJob.Data(), "job data should match")
		assert.Equal(rgj.wgc, newJob.wgc, "WaitGroup counter should be shared with the group")
		assert.Equal(rgj.Response, newJob.Response, "Response should be shared with the group")
	})

	t.Run("getting results channel", func(t *testing.T) {
		// Create a result group job
		bufferSize := 2
		rgj := newResultGroupJob[string, int](bufferSize)

		assert := assert.New(t)

		// Get results channel
		ch := rgj.Results()
		assert.NotNil(ch, "results channel should not be nil")
	})

	t.Run("closing a result group job", func(t *testing.T) {
		// Create a result group job
		bufferSize := 2
		rgj := newResultGroupJob[string, int](bufferSize)

		// Set job status to make it closeable
		rgj.changeStatus(created)

		assert := assert.New(t)

		// Close the job
		err := rgj.Close()
		assert.Nil(err, "closing result group job should not fail")
		assert.Equal("Closed", rgj.Status(), "job status should be 'Closed' after close")
		assert.True(rgj.IsClosed(), "job should be marked as closed")
	})

	t.Run("closing a result group job with all jobs complete", func(t *testing.T) {
		// Create a result group job with just one job
		bufferSize := 1
		rgj := newResultGroupJob[string, int](bufferSize)

		// Set job status to make it closeable
		rgj.changeStatus(created)

		assert := assert.New(t)

		// Mark all jobs as done
		rgj.wgc.Done()

		// Close the job
		err := rgj.Close()
		assert.Nil(err, "closing result group job should not fail")
		assert.Equal("Closed", rgj.Status(), "job status should be 'Closed' after close")
	})

	t.Run("closing a processing result group job should fail", func(t *testing.T) {
		// Create a result group job
		bufferSize := 2
		rgj := newResultGroupJob[string, int](bufferSize)

		// Set job status to processing
		rgj.changeStatus(processing)

		assert := assert.New(t)

		// Try to close the job - should fail with processing error
		err := rgj.Close()
		expectedError := errors.New("job is processing, you can't close processing job")
		assert.NotNil(err, "closing a processing job should fail")
		assert.Equal(expectedError.Error(), err.Error(), "error message should indicate job is processing")
	})

	t.Run("closing an already closed result group job should fail", func(t *testing.T) {
		// Create a result group job
		bufferSize := 2
		rgj := newResultGroupJob[string, int](bufferSize)

		// Set job status to closed
		rgj.changeStatus(closed)

		assert := assert.New(t)

		// Try to close the job - should fail with already closed error
		err := rgj.Close()
		expectedError := errors.New("job is already closed")
		assert.NotNil(err, "closing an already closed job should fail")
		assert.Equal(expectedError.Error(), err.Error(), "error message should indicate job is already closed")
	})
}

func TestErrorGroupJob(t *testing.T) {
	t.Run("error group job creation", func(t *testing.T) {
		// Create a new error group job with buffer size 3
		bufferSize := 3
		egj := newErrorGroupJob[string](bufferSize)

		assert := assert.New(t)

		// Validate error group job structure
		assert.NotNil(egj, "error group job should not be nil")
		assert.NotNil(egj.wgc, "WaitGroup counter should be initialized")
		assert.Equal(bufferSize, egj.NumPending(), "initial pending count should match buffer size")
	})

	t.Run("error group job Wait method", func(t *testing.T) {
		// Create an error group job
		bufferSize := 1
		egj := newErrorGroupJob[string](bufferSize)

		assert := assert.New(t)

		// Test Wait method in a goroutine
		done := make(chan struct{})
		go func() {
			defer close(done)
			egj.Wait() // This should block until all jobs are done
		}()

		// Try to receive - this should timeout because Wait is blocking
		select {
		case <-done:
			assert.Fail("Wait returned before jobs were completed")
		case <-time.After(100 * time.Millisecond):
			// This is expected - Wait is still blocking
		}

		// Complete all pending jobs
		for range bufferSize {
			egj.wgc.Done()
		}

		// Wait should now complete
		select {
		case <-done:
			// This is expected - Wait has completed
		case <-time.After(100 * time.Millisecond):
			assert.Fail("Wait did not return after jobs were completed")
		}
	})

	t.Run("creating new job in an error group", func(t *testing.T) {
		// Create a new error group job
		bufferSize := 3
		egj := newErrorGroupJob[string](bufferSize)

		// Create a job within the group
		jobData := "test error group data"
		jobId := "error-group-job-123"
		newJob := egj.newJob(jobData, jobConfigs{Id: jobId})

		assert := assert.New(t)

		// Validate the new job
		assert.NotNil(newJob, "new job in error group should not be nil")
		assert.Equal(generateGroupId(jobId), newJob.ID(), "job ID should have group prefix")
		assert.Equal(jobData, newJob.Data(), "job data should match")
		assert.Equal(egj.wgc, newJob.wgc, "WaitGroup counter should be shared with the group")
		assert.Equal(egj.Response, newJob.Response, "Response should be shared with the group")
	})

	t.Run("getting errors channel", func(t *testing.T) {
		// Create an error group job
		bufferSize := 2
		egj := newErrorGroupJob[string](bufferSize)

		assert := assert.New(t)

		// Get errors channel
		ch := egj.Errs()
		assert.NotNil(ch, "errors channel should not be nil")
	})

	// We're skipping the "error group job Errs error branch" test because
	// proper error simulation would require a complete reimplementation of
	// the channel behavior, which is outside the scope of these tests

	t.Run("closing an error group job", func(t *testing.T) {
		// Create an error group job
		bufferSize := 2
		egj := newErrorGroupJob[string](bufferSize)

		// Set job status to make it closeable
		egj.changeStatus(created)

		assert := assert.New(t)

		// Close the job
		err := egj.Close()
		assert.Nil(err, "closing error group job should not fail")
		assert.Equal("Closed", egj.Status(), "job status should be 'Closed' after close")
		assert.True(egj.IsClosed(), "job should be marked as closed")
	})

	t.Run("closing an error group job with all jobs complete", func(t *testing.T) {
		// Create an error group job with just one job
		bufferSize := 1
		egj := newErrorGroupJob[string](bufferSize)

		// Set job status to make it closeable
		egj.changeStatus(created)

		assert := assert.New(t)

		// Mark all jobs as done
		egj.wgc.Done()

		// Close the job
		err := egj.Close()
		assert.Nil(err, "closing error group job should not fail")
		assert.Equal("Closed", egj.Status(), "job status should be 'Closed' after close")
	})

	t.Run("closing a processing error group job should fail", func(t *testing.T) {
		// Create an error group job
		bufferSize := 2
		egj := newErrorGroupJob[string](bufferSize)

		// Set job status to processing
		egj.changeStatus(processing)

		assert := assert.New(t)

		// Try to close the job - should fail with processing error
		err := egj.Close()
		expectedError := errors.New("job is processing, you can't close processing job")
		assert.NotNil(err, "closing a processing job should fail")
		assert.Equal(expectedError.Error(), err.Error(), "error message should indicate job is processing")
	})

	t.Run("closing an already closed error group job should fail", func(t *testing.T) {
		// Create an error group job
		bufferSize := 2
		egj := newErrorGroupJob[string](bufferSize)

		// Set job status to closed
		egj.changeStatus(closed)

		assert := assert.New(t)

		// Try to close the job - should fail with already closed error
		err := egj.Close()
		expectedError := errors.New("job is already closed")
		assert.NotNil(err, "closing an already closed job should fail")
		assert.Equal(expectedError.Error(), err.Error(), "error message should indicate job is already closed")
	})
}
