package varmq

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/goptics/varmq/contract"
	"github.com/goptics/varmq/internal/queues"
	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJob(t *testing.T) {
	t.Run("job creation with newJob", func(t *testing.T) {
		// Create a new job
		jobData := "test data"
		jobId := "job-123"
		j := newJob(jobData, jobConfigs{Id: jobId})

		assert := assert.New(t)

		// Validate job structure
		assert.NotNil(j, "job should not be nil")
		assert.Equal(jobId, j.ID(), "job ID should match")
		assert.Equal(jobData, j.Data(), "job data should match")
		assert.Equal("Created", j.Status(), "job status should be 'Created'")
		assert.False(j.IsClosed(), "job should not be closed initially")
	})

	t.Run("job setAck", func(t *testing.T) {
		// Create a new job
		jobData := "test data"
		j := newJob(jobData, jobConfigs{Id: "job-ack-test"})
		assert := assert.New(t)

		assert.Nil(j.ackRef.Load(), "a fresh job carries no ack")

		mockQueue := mocks.NewMockPersistentQueue()
		j.setAck(mockQueue, "test-ack-id")
		assert.Equal("test-ack-id", j.ackRef.Load().id)

		// Now try to acknowledge - this will use both the ack id and queue we set
		err := j.acknowledge()
		assert.Nil(err, "ack should succeed against the mock queue")
	})

	t.Run("job structs stay in their size classes", func(t *testing.T) {
		// The in-memory hot path allocates one job per Add. Folding the ack
		// fields behind one pointer keeps job[T] at 56 bytes (class 64, was
		// 80); storing the error or result in place keeps errorJob at 72 and
		// resultJob at 80 (both class 80, were 96 plus a channel).
		for name, size := range map[string]uintptr{
			"job[int]":           unsafe.Sizeof(job[int]{}),
			"errorJob[int]":      unsafe.Sizeof(errorJob[int]{}),
			"resultJob[int,int]": unsafe.Sizeof(resultJob[int, int]{}),
		} {
			t.Logf("unsafe.Sizeof(%s) = %d", name, size)
		}
		assert.LessOrEqual(t, unsafe.Sizeof(job[int]{}), uintptr(64))
		assert.LessOrEqual(t, unsafe.Sizeof(errorJob[int]{}), uintptr(80))
		assert.LessOrEqual(t, unsafe.Sizeof(resultJob[int, int]{}), uintptr(80))
		assert.LessOrEqual(t, unsafe.Sizeof(jobAck{}), uintptr(48))
	})

	t.Run("job Wait method", func(t *testing.T) {
		// Create a new job but don't call the constructor
		// to avoid automatic wg initialization
		j := &job[string]{
			id:     "job-wait-test",
			data:   "test data",
			status: atomic.Uint32{},
			wg:     sync.WaitGroup{},
		}

		// Manually add to waitgroup
		j.wg.Add(1)

		assert := assert.New(t)

		// Now manually signal done to test Wait
		doneCh := make(chan struct{})
		go func() {
			defer close(doneCh)
			j.Wait() // This should block until wg is done
		}()

		// Verify Wait is blocking
		select {
		case <-doneCh:
			assert.Fail("Wait returned before wg.Done was called")
		case <-time.After(50 * time.Millisecond):
			// Expected - Wait is blocking
		}

		// Now signal completion
		j.wg.Done()

		// Now Wait should complete
		select {
		case <-doneCh:
			assert.True(true, "Wait completed after wg.Done was called")
		case <-time.After(50 * time.Millisecond):
			assert.Fail("Wait did not complete after wg.Done was called")
		}
	})

	t.Run("parseToJob function", func(t *testing.T) {
		assert := assert.New(t)

		// Test valid JSON parsing
		validJSON := []byte(`{"id":"test-id","status":"Created","data":"test data"}`)
		result, err := parseToJob(validJSON, newJob[string])
		assert.NoError(err, "parseToJob should not error with valid JSON")
		assert.NotNil(result, "parseToJob result should not be nil")

		j := result
		assert.Equal("test-id", j.ID(), "job ID should match")
		assert.Equal("test data", j.Data(), "job data should match")
		assert.Equal("Created", j.Status(), "job status should match")

		// Test each status type
		statuses := []string{"Queued", "Processing", "Finished", "Closed"}
		for _, status := range statuses {
			jsonWithStatus := []byte(`{"id":"test-id","status":"` + status + `","data":"test data"}`)
			result, err := parseToJob(jsonWithStatus, newJob[string])
			assert.NoError(err, "parseToJob should not error with status "+status)
			j := result
			assert.Equal(status, j.Status(), "job status should match "+status)
		}

		// Test invalid status
		invalidStatus := []byte(`{"id":"test-id","status":"Invalid","data":"test data"}`)
		_, err = parseToJob(invalidStatus, newJob[string])
		assert.Error(err, "parseToJob should error with invalid status")
		assert.Contains(err.Error(), "invalid status", "error should mention invalid status")

		// Test invalid JSON
		invalidJSON := []byte(`{invalid json}`)
		_, err = parseToJob(invalidJSON, newJob[string])
		assert.Error(err, "parseToJob should error with invalid JSON")
		assert.Contains(err.Error(), "failed to parse job", "error should mention parsing failure")
	})

	t.Run("job status transitions", func(t *testing.T) {
		// Create a new job
		j := newJob("test", jobConfigs{Id: "job-status"})
		assert := assert.New(t)

		// Initial status should be created
		assert.Equal("Created", j.Status(), "initial status should be 'Created'")

		// Transition to queued
		j.changeStatus(queued)
		assert.Equal("Queued", j.Status(), "status should be 'Queued' after change")

		// Transition to processing
		j.changeStatus(processing)
		assert.Equal("Processing", j.Status(), "status should be 'Processing' after change")

		// Transition to finished
		j.changeStatus(finished)
		assert.Equal("Finished", j.Status(), "status should be 'Finished' after change")

		// Transition to closed
		j.changeStatus(closed)
		assert.Equal("Closed", j.Status(), "status should be 'Closed' after change")
		assert.True(j.IsClosed(), "job should be marked as closed")

		// Test with invalid status
		j.status.Store(99) // Set an invalid status value
		assert.Equal("Unknown", j.Status(), "invalid job status should return 'Unknown'")

	})

	t.Run("job JSON serialization", func(t *testing.T) {
		// Create a new job
		j := newJob("test data", jobConfigs{Id: "job-json"})
		assert := assert.New(t)

		// Serialize to JSON
		jsonData, err := j.Json()
		assert.Nil(err, "JSON serialization should not fail")
		assert.NotEmpty(jsonData, "JSON data should not be empty")

		// We can't fully test parsing here as it's not exported,
		// but we can verify the JSON contains expected fields
		jsonStr := string(jsonData)
		assert.Contains(jsonStr, `"id":"job-json"`, "JSON should contain job ID")
		assert.Contains(jsonStr, `"data":"test data"`, "JSON should contain job data")
		assert.Regexp(`"enqueued_at":\d{13}`, jsonStr, "JSON should carry enqueued_at in Unix ms")
		assert.Contains(jsonStr, `"status":"Created"`, "JSON should contain job status")
	})

	t.Run("closing a job", func(t *testing.T) {
		// Create a new job
		j := newJob("test data", jobConfigs{Id: "job-close"})
		assert := assert.New(t)

		// Close the job
		err := j.Close()
		assert.Nil(err, "closing job should not fail")
		assert.Equal("Closed", j.Status(), "job status should be 'Closed' after close")
		assert.True(j.IsClosed(), "job should be marked as closed")

		// Attempting to close again should fail
		err = j.Close()
		assert.NotNil(err, "closing an already closed job should fail")
		assert.Contains(err.Error(), "already closed", "error message should indicate job is already closed")
	})

	t.Run("job ack with various scenarios", func(t *testing.T) {
		assert := assert.New(t)

		// Test 1: No ack attached
		j1 := newJob("test data", jobConfigs{Id: "job-ack-empty"})
		err := j1.acknowledge()
		assert.Nil(err, "ack should not fail with no ack attached")

		// Test 2: Job is closed
		j2 := newJob("test data", jobConfigs{Id: "job-ack-closed"})
		j2.setAck(mocks.NewMockPersistentQueue(), "some-ack-id")
		_ = j2.Close() // Close the job first
		err = j2.acknowledge()
		assert.Nil(err, "a closed job was already acknowledged: nothing left to do")
	})
}

func TestResultJob(t *testing.T) {
	t.Run("resultJob creation and result handling", func(t *testing.T) {
		// Create a new result job
		jobData := "test data"
		jobId := "result-job-123"
		j := newResultJob[string, int](jobData, jobConfigs{Id: jobId})

		assert := assert.New(t)

		// Validate job structure
		assert.NotNil(j, "resultJob should not be nil")
		assert.Equal(jobId, j.ID(), "job ID should match")
		assert.Equal(jobData, j.Data(), "job data should match")
		assert.Equal("Created", j.Status(), "job status should be 'Created'")

		// Save and send a result, then Close as the worker does after processing
		expectedResult := 42
		j.sendResult(expectedResult)
		assert.NoError(j.Close(), "closing job should not fail")

		// Get the result
		result, err := j.Result()
		assert.Equal(expectedResult, result, "result should match what was sent")
		assert.Nil(err, "error should be nil")
	})

	t.Run("resultJob Result after Close without a send", func(t *testing.T) {
		j := newResultJob[string, int]("test data", jobConfigs{Id: "result-job-no-send"})
		assert := assert.New(t)

		done := make(chan struct{})
		go func() {
			defer close(done)
			j.Result() // must block until Close
		}()

		select {
		case <-done:
			assert.Fail("Result returned before the job was closed")
		case <-time.After(50 * time.Millisecond):
		}

		assert.NoError(j.Close(), "closing job should not fail")
		<-done

		result, err := j.Result()
		assert.Equal(0, result, "result should be the zero value when nothing was sent")
		assert.Nil(err, "error should be nil when nothing was sent")
	})

	t.Run("resultJob error handling", func(t *testing.T) {
		// Create a new result job
		j := newResultJob[string, int]("test data", jobConfigs{Id: "result-job-error"})
		assert := assert.New(t)

		// Save and send an error, then Close as the worker does after processing
		expectedErr := errors.New("test error")
		j.sendError(expectedErr)
		assert.NoError(j.Close(), "closing job should not fail")

		// Get the result
		var zeroValue int
		result, err := j.Result()
		assert.Equal(zeroValue, result, "result should be zero value")
		assert.Equal(expectedErr, err, "error should match what was sent")
	})

	t.Run("closing a resultJob", func(t *testing.T) {
		// Create a new result job
		j := newResultJob[string, int]("test data", jobConfigs{Id: "result-job-close"})
		assert := assert.New(t)

		// Close the job
		err := j.Close()
		assert.Nil(err, "closing job should not fail")
		assert.Equal("Closed", j.Status(), "job status should be 'Closed' after close")
		assert.True(j.IsClosed(), "job should be marked as closed")
	})

	t.Run("resultJob Result after sendError", func(t *testing.T) {
		// Create a new result job
		j := newResultJob[string, int]("test data", jobConfigs{Id: "result-job-error"})
		assert := assert.New(t)

		// Send an error through the job, then Close as the worker does after processing
		expectedErr := errors.New("job closed error")
		j.sendError(expectedErr)
		assert.NoError(j.Close(), "closing job should not fail")

		// Try to get result
		result, err := j.Result()
		assert.Error(err, "Result should return an error after sendError")
		assert.Equal(expectedErr, err, "error should match what was sent")
		assert.Equal(0, result, "result should be zero value when error occurs")
	})

	t.Run("resultJob Close error cases", func(t *testing.T) {
		assert := assert.New(t)

		// Test: Closing a processing job should fail
		j1 := newResultJob[string, int]("test data", jobConfigs{Id: "result-job-processing"})
		j1.changeStatus(processing)
		err := j1.Close()
		assert.Error(err, "closing a processing job should fail")
		assert.Contains(err.Error(), "processing", "error should indicate job is processing")

		// Test: Closing an already closed job should fail
		j2 := newResultJob[string, int]("test data", jobConfigs{Id: "result-job-already-closed"})
		j2.changeStatus(closed)
		err = j2.Close()
		assert.Error(err, "closing an already closed job should fail")
		assert.Contains(err.Error(), "already closed", "error should indicate job is already closed")
	})
}

func TestErrorJob(t *testing.T) {
	t.Run("errorJob creation and error handling", func(t *testing.T) {
		// Create a new error job
		jobData := "test data"
		jobId := "error-job-123"
		j := newErrorJob(jobData, jobConfigs{Id: jobId})

		assert := assert.New(t)

		// Validate job structure
		assert.NotNil(j, "errorJob should not be nil")
		assert.Equal(jobId, j.ID(), "job ID should match")
		assert.Equal(jobData, j.Data(), "job data should match")
		assert.Equal("Created", j.Status(), "job status should be 'Created'")

		// Send an error, then Close as the worker does after processing
		expectedErr := errors.New("test error")
		j.sendError(expectedErr)
		assert.NoError(j.Close(), "closing job should not fail")

		// Get the error
		err := j.Err()
		assert.Equal(expectedErr, err, "error should match what was sent")
	})

	t.Run("errorJob Err after Close without a send", func(t *testing.T) {
		j := newErrorJob("test data", jobConfigs{Id: "error-job-no-send"})
		assert := assert.New(t)

		done := make(chan struct{})
		go func() {
			defer close(done)
			j.Err() // must block until Close
		}()

		select {
		case <-done:
			assert.Fail("Err returned before the job was closed")
		case <-time.After(50 * time.Millisecond):
		}

		assert.NoError(j.Close(), "closing job should not fail")
		<-done

		assert.Nil(j.Err(), "error should be nil when nothing was sent")
	})

	t.Run("closing an errorJob", func(t *testing.T) {
		// Create a new error job
		j := newErrorJob("test data", jobConfigs{Id: "error-job-close"})
		assert := assert.New(t)

		// Close the job
		err := j.Close()
		assert.Nil(err, "closing job should not fail")
		assert.Equal("Closed", j.Status(), "job status should be 'Closed' after close")
		assert.True(j.IsClosed(), "job should be marked as closed")
	})

	t.Run("errorJob Close error cases", func(t *testing.T) {
		assert := assert.New(t)

		// Test: Closing a processing job should fail
		j1 := newErrorJob("test data", jobConfigs{Id: "error-job-processing"})
		j1.changeStatus(processing)
		err := j1.Close()
		assert.Error(err, "closing a processing job should fail")
		assert.Contains(err.Error(), "processing", "error should indicate job is processing")

		// Test: Closing an already closed job should fail
		j2 := newErrorJob("test data", jobConfigs{Id: "error-job-already-closed"})
		j2.changeStatus(closed)
		err = j2.Close()
		assert.Error(err, "closing an already closed job should fail")
		assert.Contains(err.Error(), "already closed", "error should indicate job is already closed")
	})
}

func TestJobCloseEdgeCases(t *testing.T) {
	t.Run("Close processing job", func(t *testing.T) {
		workerFunc := func(j iJob[string]) {
			err := j.Close()
			assert.Error(t, err, "Should not be able to close processing job")
			assert.Contains(t, err.Error(), "job is processing")
		}

		mockQueue := mocks.NewMockPersistentQueue()
		worker := newWorker(workerFunc)
		queue := newPersistentQueue(worker, mockQueue)
		queue.Add("test")
		worker.Wait()
	})

	t.Run("Close already closed job", func(t *testing.T) {
		job := newJob("test", jobConfigs{})
		err := job.Close()
		assert.NoError(t, err)

		err = job.Close()
		assert.Error(t, err, "Should not be able to close already closed job")
		assert.Contains(t, err.Error(), "job is already closed")
	})
}

// A user Close racing the worker's own Close after the run must close the job
// exactly once; the loser used to hit "sync: negative WaitGroup counter".
func TestCloseRacingWorkerCompletion(t *testing.T) {
	const iterations = 3000

	t.Run("error worker", func(t *testing.T) {
		w := NewErrWorker(func(j Job[int]) error { return nil }, 4)
		defer w.StopAndWait()
		q := w.BindQueue()

		for i := range iterations {
			j, ok := q.Add(i)
			assert.True(t, ok)
			_ = j.Close()
			assert.NoError(t, j.Err())
			j.Wait()
		}
	})

	t.Run("result worker", func(t *testing.T) {
		w := NewResultWorker(func(j Job[int]) (int, error) { return j.Data(), nil }, 4)
		defer w.StopAndWait()
		q := w.BindQueue()

		for i := range iterations {
			j, ok := q.Add(i)
			assert.True(t, ok)
			_ = j.Close()
			_, err := j.Result()
			assert.NoError(t, err)
			j.Wait()
		}
	})
}

func TestConcurrentCloseClosesOnce(t *testing.T) {
	const iterations = 1000

	closers := map[string]func() (iJob[int], func() error){
		"job": func() (iJob[int], func() error) {
			j := newJob(0, jobConfigs{})
			return j, j.Close
		},
		"error job": func() (iJob[int], func() error) {
			j := newErrorJob(0, jobConfigs{})
			return j, j.Close
		},
		"result job": func() (iJob[int], func() error) {
			j := newResultJob[int, int](0, jobConfigs{})
			return j, j.Close
		},
	}

	for name, newCloser := range closers {
		t.Run(name, func(t *testing.T) {
			for range iterations {
				j, closeJob := newCloser()
				j.changeStatus(queued)

				errs := make([]error, 2)
				var start, done sync.WaitGroup
				start.Add(1)
				for i := range errs {
					done.Add(1)
					go func() {
						defer done.Done()
						start.Wait()
						errs[i] = closeJob()
					}()
				}
				start.Done()
				done.Wait()

				assert.ElementsMatch(t, []error{nil, ErrJobAlreadyClosed}, errs)
				assert.True(t, j.IsClosed())
			}
		})
	}

	t.Run("group jobs close the shared response once", func(t *testing.T) {
		for range iterations {
			gj := newErrorGroupJob[int](2)
			js := []*errorGroupJob[int]{gj.newJob(0, jobConfigs{}), gj.newJob(1, jobConfigs{})}

			var start, done sync.WaitGroup
			start.Add(1)
			for _, j := range js {
				j.changeStatus(queued)
				done.Add(1)
				go func() {
					defer done.Done()
					start.Wait()
					assert.NoError(t, j.Close())
				}()
			}
			start.Done()
			done.Wait()

			_, open := <-gj.Errs()
			assert.False(t, open)
		}
	})
}

// statusProbeQueue fails every acknowledgement and runs onAck first.
type statusProbeQueue struct{ onAck func() }

func (q statusProbeQueue) Acknowledge(string) bool {
	q.onAck()
	return false
}

func (q statusProbeQueue) Reject(id, _ string) bool { return q.Acknowledge(id) }

func (statusProbeQueue) DequeueWithAckId() (any, bool, string) { return nil, false, "" }

func TestJobAckFailure(t *testing.T) {
	// A worker that took the job can only skip it if it reads Closed. If Close
	// showed Closed during the ack and restored the state after a failure, the
	// job would be dropped by the worker yet stay open, and Wait would hang.
	t.Run("failed ack never shows the job as closed", func(t *testing.T) {
		j := newJob("test", jobConfigs{})
		j.changeStatus(queued)

		var during string
		j.setAck(statusProbeQueue{onAck: func() { during = j.Status() }}, "ack-1")

		assert.ErrorIs(t, j.Close(), ErrAcknowledgeJob)
		assert.Equal(t, "Queued", during, "job must not read Closed while it is acknowledged")
		assert.Equal(t, "Queued", j.Status())
		assert.True(t, j.startProcessing(), "the worker can still run the job")
	})

	t.Run("Ack failure from queue", func(t *testing.T) {
		mockQueue := mocks.NewMockPersistentQueue()
		mockQueue.ShouldFailAcknowledge = true

		job := newJob("test", jobConfigs{})
		job.setAck(mockQueue, "ack-1")

		err := job.acknowledge()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrAcknowledgeJob, "should return ErrAcknowledgeJob sentinel")
	})
}

func TestPublicErrors(t *testing.T) {
	t.Run("ErrJobProcessing can be detected with errors.Is", func(t *testing.T) {
		job := newJob("test", jobConfigs{})
		job.changeStatus(processing)

		err := job.Close()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrJobProcessing, "should return ErrJobProcessing when closing a processing job")
	})

	t.Run("ErrJobAlreadyClosed can be detected with errors.Is", func(t *testing.T) {
		job := newJob("test", jobConfigs{})
		err := job.Close()
		assert.NoError(t, err)

		err = job.Close()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrJobAlreadyClosed, "should return ErrJobAlreadyClosed when closing an already closed job")
	})

	t.Run("ErrAcknowledgeJob can be detected with errors.Is", func(t *testing.T) {
		mockQueue := mocks.NewMockPersistentQueue()
		mockQueue.ShouldFailAcknowledge = true

		job := newJob("test", jobConfigs{Id: "test-job"})
		job.setAck(mockQueue, "test-ack-id")

		err := job.acknowledge()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrAcknowledgeJob, "should return ErrAcknowledgeJob when acknowledgement fails")
		// Also verify the wrapped error contains useful context
		assert.Contains(t, err.Error(), "test-job", "error should contain job ID")
		assert.Contains(t, err.Error(), "test-ack-id", "error should contain ack ID")
	})

	t.Run("Close returns ErrAcknowledgeJob when ack fails", func(t *testing.T) {
		mockQueue := mocks.NewMockPersistentQueue()
		mockQueue.ShouldFailAcknowledge = true

		job := newJob("test", jobConfigs{Id: "close-ack-test"})
		job.setAck(mockQueue, "test-ack-id")

		// Call Close() which internally calls ack()
		err := job.Close()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrAcknowledgeJob, "Close should return ErrAcknowledgeJob when ack fails")
	})
}

// Drain is a no-op on single error and result jobs: they keep the outcome in
// place, so Err and Result still return it afterwards.
func TestSingleJobDrainIsNoop(t *testing.T) {
	ej := newErrorJob("x", jobConfigs{})
	ej.sendError(errors.New("boom"))
	assert.NoError(t, ej.Close())
	ej.Drain()
	assert.EqualError(t, ej.Err(), "boom")

	rj := newResultJob[string, int]("x", jobConfigs{})
	rj.sendResult(7)
	assert.NoError(t, rj.Close())
	rj.Drain()
	v, err := rj.Result()
	assert.Equal(t, 7, v)
	assert.NoError(t, err)
}

// onceAckQueue acknowledges an id once, like real storage, and runs hook
// inside the first acknowledgement.
type onceAckQueue struct {
	mu    sync.Mutex
	acked map[string]bool
	calls atomic.Int32
	hook  func()
}

func (q *onceAckQueue) Reject(id, _ string) bool { return q.Acknowledge(id) }

func (q *onceAckQueue) Acknowledge(id string) bool {
	q.calls.Add(1)
	if h := q.hook; h != nil {
		q.hook = nil
		h()
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.acked[id] {
		return false
	}
	q.acked[id] = true

	return true
}

func (*onceAckQueue) DequeueWithAckId() (any, bool, string) { return nil, false, "" }

func TestAckExactlyOnce(t *testing.T) {
	t.Run("user Close acks while the worker takes the job", func(t *testing.T) {
		j := newJob("x", jobConfigs{})
		j.changeStatus(queued)
		q := &onceAckQueue{acked: map[string]bool{}}
		q.hook = func() { assert.True(t, j.startProcessing()) } // dispatched mid-ack
		j.setAck(q, "ack-1")

		assert.ErrorIs(t, j.Close(), ErrJobProcessing)

		// The worker finishes and closes, as worker_pool.go does. The user's
		// Close already acknowledged, so this must not ack again and fail,
		// which used to strand the job in Finished with Wait blocked.
		j.changeStatus(finished)
		assert.NoError(t, j.Close())
		assert.Equal(t, int32(1), q.calls.Load(), "acknowledged once")

		done := make(chan struct{})
		go func() { j.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Wait hangs: the job was stranded")
		}
	})

	t.Run("two concurrent Close calls acknowledge once", func(t *testing.T) {
		for range 200 {
			j := newJob("x", jobConfigs{})
			j.changeStatus(queued)
			q := &onceAckQueue{acked: map[string]bool{}}
			j.setAck(q, "ack-1")

			errs := make([]error, 2)
			var start, done sync.WaitGroup
			start.Add(1)
			for i := range errs {
				done.Add(1)
				go func() {
					defer done.Done()
					start.Wait()
					errs[i] = j.Close()
				}()
			}
			start.Done()
			done.Wait()

			assert.ElementsMatch(t, []error{nil, ErrJobAlreadyClosed}, errs)
			assert.Equal(t, int32(1), q.calls.Load(), "acknowledged once")
		}
	})
}

// TestJobOutcome checks a worker reports each job's outcome to acknowledging
// storage: Acknowledge on success, Reject with the error text on failure.
func TestJobOutcome(t *testing.T) {
	t.Run("worker acknowledges success and rejects a panic", func(t *testing.T) {
		mockQueue := mocks.NewMockPersistentQueue()
		w := NewWorker(func(j Job[string]) {
			if j.Data() == "bad" {
				panic("boom")
			}
		})
		q := w.WithPersistentQueue(mockQueue)

		require.True(t, q.Add("ok"))
		require.True(t, q.Add("bad"))
		w.Wait()
		w.Stop()

		assert.Equal(t, []string{"mock-ack-id"}, mockQueue.Acked())
		rejected := mockQueue.Rejected()
		require.Len(t, rejected, 1)
		assert.Contains(t, rejected[0], "boom")
	})

	t.Run("failed Reject keeps the job open for a retry", func(t *testing.T) {
		mockQueue := mocks.NewMockPersistentQueue()
		mockQueue.ShouldFailReject = true

		j := newJob("test", jobConfigs{})
		j.setAck(mockQueue, "ack-1")
		j.fail("reason")

		assert.ErrorIs(t, j.Close(), ErrAcknowledgeJob)
		assert.Empty(t, mockQueue.Acked(), "a failed job is never acknowledged")

		mockQueue.ShouldFailReject = false
		assert.NoError(t, j.Close())
		assert.Equal(t, []string{"reason"}, mockQueue.Rejected())
	})

	t.Run("result worker rejects a returned error", func(t *testing.T) {
		mockQueue := mocks.NewMockPersistentQueue()
		w := NewResultWorker(func(j Job[string]) (int, error) {
			return 0, errors.New("nope")
		})
		q := w.WithQueue(mockQueue)

		_, ok := q.Add("x")
		require.True(t, ok)
		w.Wait()
		w.Stop()

		assert.Empty(t, mockQueue.Acked())
		assert.Equal(t, []string{"nope"}, mockQueue.Rejected())
	})

	t.Run("fail without ack storage is a no-op", func(t *testing.T) {
		j := newJob("test", jobConfigs{})
		j.fail("reason")
		assert.Nil(t, j.ackRef.Load())
		assert.NoError(t, j.Close())
	})
}

func TestEnvelopeWorkflowTag(t *testing.T) {
	t.Run("tagged job round-trips", func(t *testing.T) {
		j := newJob(1, jobConfigs{Id: "wf:w1:a"})
		j.setTask(taskTag{wf: "w1", task: "a"})

		b, err := j.Json()
		require.NoError(t, err)
		assert.Contains(t, string(b), `"wf":"w1","task":"a"`)
		assert.NotContains(t, string(b), `"undo"`)

		p, err := parseToJob(b, newJob[int])
		require.NoError(t, err)

		tag := p.taskTag()
		require.NotNil(t, tag)
		assert.Equal(t, "w1", tag.wf)
		assert.Equal(t, "a", tag.task)
		assert.False(t, tag.undo)
		assert.Nil(t, tag.store)
	})

	t.Run("undo job round-trips", func(t *testing.T) {
		j := newJob(1, jobConfigs{Id: "wf:w1:a:undo"})
		j.setTask(taskTag{wf: "w1", task: "a", undo: true})

		b, err := j.Json()
		require.NoError(t, err)
		assert.Contains(t, string(b), `"wf":"w1","task":"a","undo":true`)

		p, err := parseToJob(b, newJob[int])
		require.NoError(t, err)
		tag := p.taskTag()
		require.NotNil(t, tag)
		assert.True(t, tag.undo)
	})

	t.Run("untagged job has no wf key", func(t *testing.T) {
		b, err := newJob(1, jobConfigs{Id: "j1"}).Json()
		require.NoError(t, err)
		assert.NotContains(t, string(b), `"wf"`)
		assert.NotContains(t, string(b), `"task"`)
	})

	t.Run("tag survives setAck and fail", func(t *testing.T) {
		j := newJob(1, jobConfigs{Id: "j1"})
		j.setTask(taskTag{wf: "w1", task: "a"})
		j.setAck(mocks.NewMockPersistentQueue(), "ack1")
		j.fail("boom")
		require.NotNil(t, j.taskTag())
		ext := j.ackRef.Load().ext
		assert.Nil(t, ext.reason, "a task's failure is its end, not a Reject")
		assert.Equal(t, TaskEnd{Outcome: OutcomeFailed, Reason: "boom"}, *ext.end)
	})

	t.Run("setAck keeps the tag, not a stale reason", func(t *testing.T) {
		pq := mocks.NewMockPersistentQueue()
		j := newJob(1, jobConfigs{Id: "j1"})
		j.setAck(pq, "ack1")
		j.fail("old run")
		j.setTask(taskTag{wf: "w1", task: "a"})
		j.setAck(pq, "ack2")
		ref := j.ackRef.Load()
		assert.Equal(t, "ack2", ref.id)
		assert.Nil(t, ref.ext.reason)
		assert.Nil(t, ref.ext.end)
		assert.Equal(t, "a", ref.ext.task.task)
	})

	t.Run("failed task on storage that can't finish tasks is rejected", func(t *testing.T) {
		pq := mocks.NewMockPersistentQueue()
		j := newJob(1, jobConfigs{Id: "j1"})
		j.setTask(taskTag{wf: "w1", task: "a"})
		j.setAck(pq, "ack1")
		j.fail("boom")
		require.NoError(t, j.Close())
		assert.Equal(t, []string{"boom"}, pq.Rejected())
		assert.Empty(t, pq.Acked())
	})

	t.Run("Close of a tag-only job", func(t *testing.T) {
		j := newJob(1, jobConfigs{Id: "j1"})
		j.setTask(taskTag{wf: "w1", task: "a"}) // no ack id, no store
		assert.NotPanics(t, func() { assert.NoError(t, j.Close()) })
		assert.True(t, j.IsClosed())
	})
}

// TestEnvelopeWorkflowTagInvalid: an envelope whose workflow fields could
// not come from a Submit is unparsable, so no store key is built from it.
func TestEnvelopeWorkflowTagInvalid(t *testing.T) {
	for name, env := range map[string]string{
		"wf without task":    `{"id":"wf:w1:","status":"Created","data":1,"wf":"w1"}`,
		"task without wf":    `{"id":"x","status":"Created","data":1,"task":"a"}`,
		"undo without wf":    `{"id":"x","status":"Created","data":1,"undo":true}`,
		"wf index":           `{"id":"wf:index:a","status":"Created","data":1,"wf":"index","task":"a"}`,
		"wf bad char":        `{"id":"wf:w 1:a","status":"Created","data":1,"wf":"w 1","task":"a"}`,
		"wf with colon":      `{"id":"wf:w:1:a","status":"Created","data":1,"wf":"w:1","task":"a"}`,
		"wf too long":        `{"id":"wf:` + strings.Repeat("w", 129) + `:a","status":"Created","data":1,"wf":"` + strings.Repeat("w", 129) + `","task":"a"}`,
		"task bad char":      `{"id":"wf:w1:a/b","status":"Created","data":1,"wf":"w1","task":"a/b"}`,
		"task too long":      `{"id":"wf:w1:` + strings.Repeat("a", 65) + `","status":"Created","data":1,"wf":"w1","task":"` + strings.Repeat("a", 65) + `"}`,
		"id mismatch":        `{"id":"order-1","status":"Created","data":1,"wf":"w1","task":"a"}`,
		"id of another task": `{"id":"wf:w1:b","status":"Created","data":1,"wf":"w1","task":"a"}`,
		"undo id lacks undo": `{"id":"wf:w1:a","status":"Created","data":1,"wf":"w1","task":"a","undo":true}`,
		"task id has undo":   `{"id":"wf:w1:a:undo","status":"Created","data":1,"wf":"w1","task":"a"}`,
	} {
		_, err := parseToJob([]byte(env), newJob[int])
		assert.ErrorIs(t, err, ErrParseJob, name)
	}

	for _, env := range []string{
		`{"id":"wf:w1:a","status":"Created","data":1,"wf":"w1","task":"a"}`,
		`{"id":"wf:w1:a:undo","status":"Created","data":1,"wf":"w1","task":"a","undo":true}`,
		`{"id":"wf:` + strings.Repeat("w", 128) + `:` + strings.Repeat("a", 64) + `","status":"Created","data":1,"wf":"` + strings.Repeat("w", 128) + `","task":"` + strings.Repeat("a", 64) + `"}`,
		`{"id":"plain","status":"Created","data":1,"undo":false}`,
	} {
		_, err := parseToJob([]byte(env), newJob[int])
		assert.NoError(t, err, env)
	}
}

func TestJobAckSizeClass(t *testing.T) {
	assert.LessOrEqual(t, unsafe.Sizeof(jobAck{}), uintptr(48))
}

func TestCancelWaitingJob(t *testing.T) {
	ran := make(chan int, 2)
	w := NewErrWorker(func(j Job[int]) error { ran <- j.Data(); return nil })
	w.Pause()
	defer w.Stop()

	q := w.BindQueue()
	j, _ := q.Add(1)
	require.NoError(t, j.Cancel())
	assert.ErrorIs(t, j.Err(), ErrJobCancelled)
	assert.ErrorIs(t, j.Cancel(), ErrJobAlreadyClosed)

	w.Resume()
	j2, _ := q.Add(2)
	require.NoError(t, j2.Err())
	assert.Equal(t, 2, <-ran, "the cancelled job never ran")
}

func TestCancelRunningJob(t *testing.T) {
	started := make(chan struct{})
	w := NewResultWorker(func(j Job[int]) (int, error) {
		close(started)
		<-j.Context().Done()
		return 0, j.Context().Err()
	})
	defer w.Stop()

	j, _ := w.BindQueue().Add(1)
	<-started
	require.NoError(t, j.Cancel())
	_, err := j.Result()
	assert.ErrorIs(t, err, ErrJobCancelled)
	assert.Zero(t, liveRuns.Load(), "run state dropped")
}

func TestCancelIgnoredFinishesNormally(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	w := NewResultWorker(func(j Job[int]) (int, error) {
		close(started)
		<-release // never looks at Context
		return 7, nil
	})
	defer w.Stop()

	j, _ := w.BindQueue().Add(1)
	<-started
	require.NoError(t, j.Cancel())
	close(release)
	r, err := j.Result()
	require.NoError(t, err)
	assert.Equal(t, 7, r)
	assert.Zero(t, liveRuns.Load())
}

func TestContextOutsideRun(t *testing.T) {
	j := newJob(1, jobConfigs{})
	assert.Equal(t, context.Background(), j.Context())
	assert.Zero(t, liveRuns.Load())
}

// cancelStore is sharedStore with cancel by ID, as redisq does it: a mark
// that drops the job at pop (no workflow store: wf gets no exception), a notice to consumers, a
// cancelled finish; a re-add clears the id's mark and outcome (spec 6.13,
// 6.14 Re-add).
type cancelStore struct {
	*sharedStore
	cmu    sync.Mutex
	marked map[string]bool
	hooks  []*func(string)
}

func newCancelStore() *cancelStore {
	return &cancelStore{sharedStore: newSharedStore(), marked: map[string]bool{}}
}

func (c *cancelStore) CancelJob(id string) error {
	c.mu.Lock()
	_, ended := c.outcomes[id]
	c.mu.Unlock()
	if ended {
		return contract.ErrJobEnded
	}
	c.cmu.Lock()
	c.marked[id] = true
	var hooks []func(string)
	for _, p := range c.hooks {
		hooks = append(hooks, *p)
	}
	c.cmu.Unlock()
	for _, fn := range hooks {
		fn(id)
	}
	return nil
}

func (c *cancelStore) OnCancel(fn func(string)) func() {
	p := &fn
	c.cmu.Lock()
	c.hooks = append(c.hooks, p)
	c.cmu.Unlock()
	return func() {
		c.cmu.Lock()
		defer c.cmu.Unlock()
		c.hooks = slices.DeleteFunc(c.hooks, func(x *func(string)) bool { return x == p })
	}
}

func (c *cancelStore) EnqueueOnce(item any, id string) bool {
	c.mu.Lock()
	delete(c.outcomes, id)
	c.mu.Unlock()
	c.cmu.Lock()
	delete(c.marked, id)
	c.cmu.Unlock()
	return c.push(item, 0)
}

func (c *cancelStore) DequeueWithAckId() (any, bool, string) {
	for {
		v, ok, ack := c.sharedStore.DequeueWithAckId()
		if !ok {
			return v, ok, ack
		}
		var env struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(v.([]byte), &env)
		c.cmu.Lock()
		drop := c.marked[env.ID]
		delete(c.marked, env.ID)
		c.cmu.Unlock()
		if !drop {
			return v, ok, ack
		}
		c.finish(ack, JobCancelled, "cancelled", nil)
	}
}

func (c *cancelStore) Dequeue() (any, bool) { v, ok, _ := c.DequeueWithAckId(); return v, ok }

func (c *cancelStore) AcknowledgeCancelled(ackID, reason string) bool {
	c.mu.Lock()
	id := c.pending[ackID]
	c.mu.Unlock()
	c.cmu.Lock()
	delete(c.marked, id)
	c.cmu.Unlock()
	return c.finish(ackID, JobCancelled, reason, nil)
}

var (
	_ JobCanceller          = (*cancelStore)(nil)
	_ CancelNotifier        = (*cancelStore)(nil)
	_ CancelAcknowledgeable = (*cancelStore)(nil)
)

func TestRemoteCancelRunning(t *testing.T) {
	s := newCancelStore()
	started := make(chan struct{})
	w := NewErrWorker(func(j Job[int]) error {
		close(started)
		<-j.Context().Done()
		return j.Context().Err()
	})
	defer w.Stop()
	w.WithQueue(s) // consumer

	j, _ := NewErrQueue[int](s).Add(1) // producer
	<-started
	require.NoError(t, OpenErrJob(s, j.ID()).Cancel(), "from any handle")
	assert.ErrorIs(t, j.Err(), ErrJobCancelled)
	assert.ErrorIs(t, j.Cancel(), ErrJobAlreadyClosed)
}

func TestRemoteCancelWaiting(t *testing.T) {
	s := newCancelStore()
	p := NewErrQueue[int](s)
	j, _ := p.Add(1) // no consumer yet
	require.NoError(t, j.Cancel())

	ran := make(chan int, 1)
	w := NewErrWorker(func(j Job[int]) error { ran <- j.Data(); return nil })
	defer w.Stop()
	w.WithQueue(s)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, j.WaitContext(ctx))
	assert.ErrorIs(t, j.Err(), ErrJobCancelled)
	assert.Empty(t, ran, "dropped at pop, never ran")
}

func TestRemoteCancelUnsupported(t *testing.T) {
	j, _ := NewQueue[int](newSharedStore()).Add(1)
	assert.ErrorIs(t, j.Cancel(), ErrCancelUnsupported)
}

func TestCancelWaitingDurableJobFinishesInStorage(t *testing.T) {
	s := newCancelStore()
	h, _ := NewErrQueue[int](s).Add(1) // producer handle
	_, ok, ack := s.sharedStore.DequeueWithAckId()
	require.True(t, ok)

	// the consumer's copy, taken but not started (as in a worker's batch)
	j := newErrorJob(1, jobConfigs{Id: h.ID()})
	j.setAck(s, ack)
	j.markQueued()

	require.NoError(t, j.Cancel())
	assert.ErrorIs(t, j.Err(), ErrJobCancelled)
	assert.ErrorIs(t, h.Err(), ErrJobCancelled, "storage finished it cancelled: no redelivery")
}

func TestRemoteCancelScopedToItsStorage(t *testing.T) {
	a, b := newCancelStore(), newCancelStore()
	started := make(chan struct{})
	release := make(chan struct{})
	w := NewErrWorker(func(j Job[int]) error {
		close(started)
		select {
		case <-j.Context().Done():
			return j.Context().Err()
		case <-release:
			return nil
		}
	})
	defer w.Stop()
	w.WithQueue(a)
	w.WithQueue(b)

	jb, _ := NewErrQueue[int](b).Add(1, WithJobId("same"))
	<-started
	require.NoError(t, OpenErrJob(a, "same").Cancel(), "a request on queue a")
	close(release)
	assert.NoError(t, jb.Err(), "queue b's job with the same ID is not cancelled")
}

// cancelPrio is cancelStore as priority storage.
type cancelPrio struct{ *cancelStore }

func (p cancelPrio) Enqueue(item any, prio int) bool { return p.push(item, prio) }
func (p cancelPrio) EnqueueOnce(item any, prio int, id string) bool {
	p.mu.Lock()
	delete(p.outcomes, id)
	p.mu.Unlock()
	p.cmu.Lock()
	delete(p.marked, id)
	p.cmu.Unlock()
	return p.push(item, prio)
}

// blockOnCancel is a worker function that reports its start and returns
// once its job's context is cancelled.
func blockOnCancel(started chan<- string) func(j Job[int]) {
	return func(j Job[int]) {
		started <- j.ID()
		<-j.Context().Done()
	}
}

func TestRemoteCancelReachesDeprecatedBinders(t *testing.T) {
	type adder = func(id string) bool
	binds := map[string]func(IWorkerBinder[int], *cancelStore) adder{
		"persistent": func(b IWorkerBinder[int], s *cancelStore) adder {
			q := b.WithPersistentQueue(s)
			return func(id string) bool { return q.Add(1, WithJobId(id)) }
		},
		"persistent priority": func(b IWorkerBinder[int], s *cancelStore) adder {
			q := b.WithPersistentPriorityQueue(cancelPrio{s})
			return func(id string) bool { return q.Add(1, 0, WithJobId(id)) }
		},
		"distributed": func(b IWorkerBinder[int], s *cancelStore) adder {
			b.WithDistributedQueue(s)
			return func(id string) bool { return NewDistributedQueue[int](s).Add(1, WithJobId(id)) }
		},
	}
	for name, bind := range binds {
		t.Run(name, func(t *testing.T) {
			s := newCancelStore()
			started := make(chan string, 1)
			w := NewWorker(blockOnCancel(started))
			defer w.Stop()
			add := bind(w, s)

			require.True(t, add("d"))
			<-started
			require.NoError(t, OpenJob(s, "d").Cancel())
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			assert.NoError(t, OpenJob(s, "d").WaitContext(ctx), "the running job saw the cancel")
		})
	}
}

func TestCancelThenPanic(t *testing.T) {
	s := newCancelStore()
	started := make(chan struct{})
	w := NewWorker(func(j Job[int]) {
		close(started)
		<-j.Context().Done()
		panic("after cancel")
	})
	defer w.Stop()
	w.WithQueue(s)

	j, _ := NewQueue[int](s).Add(1)
	<-started
	require.NoError(t, j.Cancel())
	j.Wait()
	assert.ErrorIs(t, <-w.Errs(), ErrJobCancelled)
	assert.ErrorIs(t, OpenErrJob(s, j.ID()).Err(), ErrJobCancelled, "storage finished it cancelled")
}

func TestRemoteCancelThenReAdd(t *testing.T) {
	s := newCancelStore()
	started := make(chan int, 2)
	release := make(chan struct{})
	w := NewErrWorker(func(j Job[int]) error {
		started <- j.Data()
		select {
		case <-j.Context().Done():
			return j.Context().Err()
		case <-release:
			return nil
		}
	})
	defer w.Stop()
	w.WithQueue(s)
	p := NewErrQueue[int](s)

	old, _ := p.Add(1, WithJobId("x"))
	<-started
	require.NoError(t, old.Cancel())
	require.ErrorIs(t, old.Err(), ErrJobCancelled)

	// the same ID again: the re-add clears the mark, so it runs
	readded, _ := p.Add(2, WithJobId("x"))
	assert.Equal(t, 2, <-started)
	assert.ErrorIs(t, old.Cancel(), ErrJobAlreadyClosed, "the old handle does not cancel the new job")
	close(release)
	assert.NoError(t, readded.Err())
}

func TestRemoteCancelEveryRunOfAnID(t *testing.T) {
	s := newCancelStore()
	started, stopped := make(chan string, 2), make(chan struct{}, 2)
	w := NewWorker(func(j Job[int]) {
		started <- j.ID()
		<-j.Context().Done()
		stopped <- struct{}{}
	}, WithConcurrency(2))
	defer w.Stop()
	w.WithQueue(s)

	// two live runs of one ID on one worker (a redelivery, a reused ID)
	p := NewQueue[int](s)
	p.Add(1, WithJobId("dup"))
	p.Add(2, WithJobId("dup"))
	<-started
	<-started
	require.NoError(t, OpenJob(s, "dup").Cancel())
	for range 2 {
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
			t.Fatal("a run of the ID missed the cancel")
		}
	}
}

// mapped is value-type storage whose type is comparable but whose value
// is not: its interface field holds a map.
type mapped struct {
	*cancelStore
	tag any
}

func TestUnhashableStorageGetsNoCancelWatch(t *testing.T) {
	plain, odd := newCancelStore(), mapped{newCancelStore(), map[string]int{}}
	started := make(chan string, 1)
	release := make(chan struct{})
	w := NewErrWorker(func(j Job[int]) error {
		started <- j.ID()
		select {
		case <-j.Context().Done():
			return j.Context().Err()
		case <-release:
			return nil
		}
	})
	defer w.Stop()
	w.WithQueue(plain) // turns the watch on
	q := w.WithQueue(odd)

	j, ok := q.Add(1)
	require.True(t, ok)
	<-started // tracking it did not panic
	require.NoError(t, OpenErrJob(odd, j.ID()).Cancel(), "requested")
	close(release)
	assert.NoError(t, j.Err(), "not reached: ran to the end")
}

// A worker's cancel watch (shared storage) does not track its in-memory
// jobs: only a listed memQueue does (listMem).
func TestCancelWatchSkipsUnlistedInMemoryJobs(t *testing.T) {
	w := NewWorker(func(Job[int]) {})
	defer w.Stop()
	w.WithQueue(newCancelStore()) // the watch is on
	q := w.BindQueue()            // unnamed worker: not listed
	j, _ := q.Add(1, WithJobId("m"))
	j.Wait()
	assert.Equal(t, "Closed", j.Status())
	assert.Nil(t, memOf(t, q).running, "never tracked")
}

func TestInMemoryRunsTrackedOnlyWhenListed(t *testing.T) {
	NewServerMux() // tracking needs a REST mux
	started := make(chan string, 1)
	unnamed := NewWorker(blockOnCancel(started))
	t.Cleanup(func() { unnamed.Stop() })
	um := memOf(t, unnamed.BindQueue())
	assert.False(t, um.listed.Load(), "not listed for REST: the run loop tracks nothing")
	assert.False(t, unnamed.(*workerBinder[int]).cancelWatch.Load())

	named := NewWorker(blockOnCancel(started), WithName("tracked-runs"))
	t.Cleanup(func() { named.Stop() })
	q := named.BindPriorityQueue()
	m := memOf(t, q)
	assert.True(t, m.listed.Load(), "listed: runs with an ID are tracked")
	assert.False(t, named.(*workerBinder[int]).cancelWatch.Load(), "the worker's watch stays off")

	ctx := context.Background()
	run, _ := q.Add(1, 0, WithJobId("r"))
	require.Equal(t, "r", within(t, started))
	wait, _ := q.Add(2, 0, WithJobId("w"))

	rec, err := m.Job(ctx, "r")
	require.NoError(t, err)
	assert.Equal(t, JobProcessing, rec.State)
	rec, err = m.Job(ctx, "w")
	require.NoError(t, err)
	assert.Equal(t, JobPending, rec.State)
	_, err = m.Job(ctx, "")
	assert.ErrorIs(t, err, ErrJobNotFound)

	require.NoError(t, m.CancelJob("w"), "waiting: closed")
	assert.True(t, wait.IsClosed())
	assert.ErrorIs(t, m.CancelJob("w"), contract.ErrJobEnded, "closed, still in the queue")
	require.NoError(t, m.CancelJob("r"), "running: context cancelled")
	run.Wait()
	assert.ErrorIs(t, m.CancelJob("r"), ErrJobNotFound, "ended jobs are not kept")
	_, err = m.Job(ctx, "r")
	assert.ErrorIs(t, err, ErrJobNotFound)
}

// A job closed while it waits in a batch is tracked, skipped, and untracked.
func TestInMemorySkippedBatchJobUntracked(t *testing.T) {
	NewServerMux()
	started := make(chan string, 1)
	w := NewWorker(blockOnCancel(started), WithName("tracked-skip"), 1)
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue()
	m := memOf(t, q)

	require.NoError(t, w.Pause())
	q.Add(1, WithJobId("a"))
	jb, _ := q.Add(2, WithJobId("b"))
	require.NoError(t, w.Resume()) // one batch: b waits behind a
	require.Equal(t, "a", within(t, started))
	require.NoError(t, jb.Close())
	require.NoError(t, m.CancelJob("a"))
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	require.NoError(t, w.WaitContext(ctx))

	m.runMu.Lock()
	assert.Empty(t, m.running)
	assert.Empty(t, m.extra)
	m.runMu.Unlock()
	assert.ErrorIs(t, m.CancelJob("b"), ErrJobNotFound)
}

// Two live runs under one ID are tracked apart: the first ending leaves
// the second readable and cancellable.
func TestInMemoryDuplicateIDRunsTrackedApart(t *testing.T) {
	NewServerMux()
	started := make(chan int, 2)
	release := make(chan struct{})
	w := NewWorker(func(j Job[int]) {
		started <- j.Data()
		if j.Data() == 1 {
			<-release
			return
		}
		<-j.Context().Done()
	}, WithName("tracked-dup"), 2)
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue()
	m := memOf(t, q)

	q.Add(1, WithJobId("d"))
	require.Equal(t, 1, within(t, started))
	run2, ok := q.Add(2, WithJobId("d")) // the first left the queue: allowed
	require.True(t, ok)
	require.Equal(t, 2, within(t, started))
	close(release)
	require.Eventually(t, func() bool { return len(m.runsOf("d")) == 1 }, time.Second, time.Millisecond)

	rec, err := m.Job(context.Background(), "d")
	require.NoError(t, err)
	assert.Equal(t, JobProcessing, rec.State)
	require.NoError(t, m.CancelJob("d"))
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	assert.NoError(t, run2.WaitContext(ctx))
}

// Without a REST mux nothing is tracked, even on a listed queue.
func TestInMemoryUntrackedWithoutMux(t *testing.T) {
	old := served.Load()
	served.Store(false)
	t.Cleanup(func() { served.Store(old) })
	started := make(chan string, 1)
	w := NewWorker(blockOnCancel(started), WithName("untracked-nomux"))
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue()
	m := memOf(t, q)
	require.True(t, m.listed.Load())

	j, _ := q.Add(1, WithJobId("n"))
	within(t, started)
	assert.Nil(t, m.runsOf("n"))
	_, err := m.Job(context.Background(), "n")
	assert.ErrorIs(t, err, ErrJobNotFound)
	require.NoError(t, j.Cancel())
	j.Wait()
}

// Untracking any of three runs under one ID keeps the others; the last
// leaves both maps empty.
func TestMemQueueUntrackOrders(t *testing.T) {
	for _, order := range [][3]int{{1, 0, 2}, {0, 1, 2}, {2, 1, 0}, {0, 2, 1}} {
		m := &memQueue{}
		js := []any{newJob(1, jobConfigs{}), newJob(2, jobConfigs{}), newJob(3, jobConfigs{})}
		for _, j := range js {
			m.track("d", j)
		}
		require.Len(t, m.runsOf("d"), 3)
		for n, i := range order {
			m.untrack("d", js[i])
			runs := m.runsOf("d")
			assert.Len(t, runs, 2-n, "order %v", order)
			assert.False(t, slices.Contains(runs, js[i]), "order %v: run %d gone", order, i)
		}
		assert.Empty(t, m.running)
		assert.Empty(t, m.extra)
	}
}

// find skips values that are no memJob; CancelJob reports a run's own
// cancel error over ErrJobEnded.
type failingMemJob struct{ m *memQueue }

func (f failingMemJob) recordOf() (string, any) { return "x", nil }
func (failingMemJob) phase() status             { return processing }
func (f failingMemJob) memTotals() *memQueue    { return f.m }
func (failingMemJob) Cancel() error             { return errors.New("boom") }

func TestMemQueueCancelJobOddRuns(t *testing.T) {
	m := &memQueue{q: queues.NewQueue[any]()}
	m.track("x", "not a job")
	_, err := m.Job(context.Background(), "x")
	require.ErrorIs(t, err, ErrJobNotFound)

	m.track("x", failingMemJob{m})
	require.EqualError(t, m.CancelJob("x"), "boom")
}

// CancelJob reaches every live run of an ID.
func TestInMemoryCancelReachesDuplicateRuns(t *testing.T) {
	NewServerMux()
	started := make(chan string, 2)
	w := NewWorker(blockOnCancel(started), WithName("tracked-dup-cancel"), 2)
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue()
	m := memOf(t, q)

	r1, _ := q.Add(1, WithJobId("d"))
	within(t, started)
	r2, ok := q.Add(2, WithJobId("d"))
	require.True(t, ok)
	within(t, started)
	require.NoError(t, m.CancelJob("d"))
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	assert.NoError(t, r1.WaitContext(ctx))
	assert.NoError(t, r2.WaitContext(ctx))
}

// cancelOnly can cancel by ID but reports no outcomes.
type cancelOnly struct{ *cancelStore }

func (cancelOnly) WatchOutcome() {}

func TestCancelOnlyStorageGetsAnID(t *testing.T) {
	j, ok := NewErrQueue[int](cancelOnly{newCancelStore()}).Add(1)
	require.True(t, ok)
	assert.NotEmpty(t, j.ID(), "an ID to cancel by")
	assert.ErrorIs(t, j.Err(), ErrDetached)
	assert.NoError(t, j.Cancel(), "a detached handle still cancels by ID")

	// ended on storage, unseen by this detached handle: storage says so
	s := newCancelStore()
	s.outcomes["e"] = JobOutcome{ID: "e", State: JobCompleted}
	assert.ErrorIs(t, watchRemote[struct{}](cancelOnly{s}, "e", nil).Cancel(), ErrJobAlreadyClosed)
}

// failingCancel can't reach its backend.
type failingCancel struct{ *sharedStore }

func (failingCancel) CancelJob(string) error { return errors.New("down") }

func TestRemoteCancelError(t *testing.T) {
	j, _ := NewQueue[int](failingCancel{newSharedStore()}).Add(1)
	assert.EqualError(t, j.Cancel(), "down")
}

func TestCancelEveryJobKind(t *testing.T) {
	queued := func(m interface{ markQueued() }) { m.markQueued() }

	j := newJob(1, jobConfigs{})
	queued(j)
	require.NoError(t, j.Cancel())
	assert.ErrorIs(t, j.Cancel(), ErrJobAlreadyClosed)

	rj := newResultJob[int, int](1, jobConfigs{})
	queued(rj)
	require.NoError(t, rj.Cancel())
	_, err := rj.Result()
	assert.ErrorIs(t, err, ErrJobCancelled)

	g := newGroupJob[int](1).newJob(1, jobConfigs{})
	queued(g)
	require.NoError(t, g.Cancel())
	g.Wait()

	eg := newErrorGroupJob[int](1)
	em := eg.newJob(1, jobConfigs{})
	queued(em)
	require.NoError(t, em.Cancel())
	for range eg.Errs() { // closed by the last member's release
	}

	rg := newResultGroupJob[int, int](1)
	rm := rg.newJob(1, jobConfigs{})
	queued(rm)
	require.NoError(t, rm.Cancel())
	for range rg.Results() {
	}
}

func TestCancelWaitingWorkflowTask(t *testing.T) {
	j := newErrorJob(1, jobConfigs{Id: "wf:w1:a"})
	j.setTask(taskTag{wf: "w1", task: "a"}) // no store: nothing to report to
	j.markQueued()
	require.NoError(t, j.Cancel())
	assert.ErrorIs(t, j.Err(), ErrJobCancelled)

	end := j.ackRef.Load().ext.end
	require.NotNil(t, end)
	assert.Equal(t, OutcomeCancelled, end.Outcome)
}

func TestEndRunWithNoRuns(t *testing.T) {
	j := newJob(1, jobConfigs{})
	if liveRuns.Load() == 0 {
		j.endRun() // nothing to drop
	}
	assert.False(t, j.cancelRequested())
}

// panicky panics when it is encoded.
type panicky struct{}

func (panicky) MarshalJSON() ([]byte, error) { panic("encode") }

func TestResultMarshalPanicFailsJob(t *testing.T) {
	s := newSharedStore()
	w := NewResultWorker(func(Job[int]) (panicky, error) { return panicky{}, nil })
	defer w.Stop()

	j, _ := w.WithQueue(s).Add(1)
	_, err := j.Result()
	var re *RemoteError
	require.ErrorAs(t, err, &re, "the worker survived and failed the job")
	assert.True(t, strings.Contains(re.Reason, "encode"), re.Reason)

	// a workflow task: the task fails
	tj := newResultJob[int, panicky](1, jobConfigs{Id: "wf:w1:a"})
	tj.setTask(taskTag{wf: "w1", task: "a"})
	require.ErrorContains(t, tj.succeedResult(), "encode")

	// in memory (no ack): nothing is encoded
	mj := newResultJob[int, panicky](1, jobConfigs{})
	require.NoError(t, mj.succeedResult())
	assert.Nil(t, mj.ackRef.Load())

	// the worker counts it failed and reports it on Errs
	mw := NewResultWorker(func(Job[int]) (panicky, error) { return panicky{}, nil })
	defer mw.Stop()
	ms := newSharedStore()
	errs := mw.Errs()
	mj2, _ := mw.WithQueue(ms).Add(1)
	_, err = mj2.Result()
	require.Error(t, err)
	select {
	case e := <-errs:
		assert.ErrorContains(t, e, "encode")
	case <-time.After(time.Second):
		t.Fatal("no error on Errs")
	}
	assert.EqualValues(t, 1, mw.Metrics().Failed())
	assert.EqualValues(t, 0, mw.Metrics().Successful())
}
