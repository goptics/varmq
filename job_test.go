package varmq

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
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

		assert.Nil(j.ackRef, "a fresh job carries no ack")

		mockQueue := mocks.NewMockPersistentQueue()
		j.setAck(mockQueue, "test-ack-id")
		assert.Equal("test-ack-id", j.ackRef.id)

		// Now try to acknowledge - this will use both the ack id and queue we set
		err := j.ack()
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
		result, err := parseToJob[string](validJSON)
		assert.NoError(err, "parseToJob should not error with valid JSON")
		assert.NotNil(result, "parseToJob result should not be nil")

		j, ok := result.(*job[string])
		assert.True(ok, "result should be a *job[string]")
		assert.Equal("test-id", j.ID(), "job ID should match")
		assert.Equal("test data", j.Data(), "job data should match")
		assert.Equal("Created", j.Status(), "job status should match")

		// Test each status type
		statuses := []string{"Queued", "Processing", "Finished", "Closed"}
		for _, status := range statuses {
			jsonWithStatus := []byte(`{"id":"test-id","status":"` + status + `","data":"test data"}`)
			result, err := parseToJob[string](jsonWithStatus)
			assert.NoError(err, "parseToJob should not error with status "+status)
			j, _ := result.(*job[string])
			assert.Equal(status, j.Status(), "job status should match "+status)
		}

		// Test invalid status
		invalidStatus := []byte(`{"id":"test-id","status":"Invalid","data":"test data"}`)
		_, err = parseToJob[string](invalidStatus)
		assert.Error(err, "parseToJob should error with invalid status")
		assert.Contains(err.Error(), "invalid status", "error should mention invalid status")

		// Test invalid JSON
		invalidJSON := []byte(`{invalid json}`)
		_, err = parseToJob[string](invalidJSON)
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
		err := j1.ack()
		assert.Nil(err, "ack should not fail with no ack attached")

		// Test 2: Job is closed
		j2 := newJob("test data", jobConfigs{Id: "job-ack-closed"})
		j2.setAck(mocks.NewMockPersistentQueue(), "some-ack-id")
		_ = j2.Close() // Close the job first
		err = j2.ack()
		assert.Nil(err, "ack should not fail on closed job")
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

func TestJobAckFailure(t *testing.T) {
	t.Run("Ack failure from queue", func(t *testing.T) {
		mockQueue := mocks.NewMockPersistentQueue()
		mockQueue.ShouldFailAcknowledge = true

		job := newJob("test", jobConfigs{})
		job.setAck(mockQueue, "ack-1")

		err := job.ack()
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

		err := job.ack()
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
