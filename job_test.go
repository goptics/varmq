package varmq

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
		assert.Equal(jobData, j.Payload(), "job data should match")
		assert.Equal("Created", j.Status(), "job status should be 'Created'")
		assert.False(j.IsClosed(), "job should not be closed initially")
	})

	t.Run("job setAckId and setInternalQueue", func(t *testing.T) {
		// Create a new job
		jobData := "test data"
		j := newJob(jobData, jobConfigs{Id: "job-ack-test"})
		assert := assert.New(t)

		// Test setAckId
		ackId := "test-ack-id"
		j.setAckId(ackId)
		// We can't directly assert j.ackId since it's private, but we can test it indirectly through other methods

		// Test setInternalQueue
		mockQueue := getNullQueue()
		j.setInternalQueue(mockQueue)
		// We can't directly assert j.queue since it's private, but we can test it indirectly through other methods

		// Now try to acknowledge - this will use both the ackId and queue we set
		// Note: This will fail since the null queue doesn't implement IAcknowledgeable
		err := j.ack()
		assert.Error(err, "ack should fail with null queue")
	})

	t.Run("job Wait method", func(t *testing.T) {
		// Create a new job but don't call the constructor
		// to avoid automatic wg initialization
		j := &job[string]{
			id:      "job-wait-test",
			payload: "test data",
			status:  atomic.Uint32{},
			wg:      sync.WaitGroup{},
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
		validJSON := []byte(`{"id":"test-id","status":"Created","payload":"test payload"}`)
		result, err := parseToJob[string](validJSON)
		assert.NoError(err, "parseToJob should not error with valid JSON")
		assert.NotNil(result, "parseToJob result should not be nil")

		j, ok := result.(*job[string])
		assert.True(ok, "result should be a *job[string]")
		assert.Equal("test-id", j.ID(), "job ID should match")
		assert.Equal("test payload", j.Payload(), "job payload should match")
		assert.Equal("Created", j.Status(), "job status should match")

		// Test each status type
		statuses := []string{"Queued", "Processing", "Finished", "Closed"}
		for _, status := range statuses {
			jsonWithStatus := []byte(`{"id":"test-id","status":"` + status + `","payload":"test payload"}`)
			result, err := parseToJob[string](jsonWithStatus)
			assert.NoError(err, "parseToJob should not error with status "+status)
			j, _ := result.(*job[string])
			assert.Equal(status, j.Status(), "job status should match "+status)
		}

		// Test invalid status
		invalidStatus := []byte(`{"id":"test-id","status":"Invalid","payload":"test payload"}`)
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
		assert.Contains(jsonStr, `"payload":"test data"`, "JSON should contain job payload")
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

		// Test 1: Empty ackId
		j1 := newJob("test data", jobConfigs{Id: "job-ack-empty"})
		err := j1.ack()
		assert.Error(err, "ack should fail with empty ackId")
		assert.Contains(err.Error(), "not acknowledgeable", "error should mention not acknowledgeable")

		// Test 2: Job is closed
		j2 := newJob("test data", jobConfigs{Id: "job-ack-closed"})
		j2.setAckId("some-ack-id")
		_ = j2.Close() // Close the job first
		err = j2.ack()
		assert.Error(err, "ack should fail on closed job")
		assert.Contains(err.Error(), "not acknowledgeable", "error should mention not acknowledgeable")

		// Test 3: Queue doesn't implement IAcknowledgeable
		j3 := newJob("test data", jobConfigs{Id: "job-ack-no-impl"})
		j3.setAckId("some-ack-id")
		j3.setInternalQueue(getNullQueue()) // Null queue doesn't implement IAcknowledgeable
		err = j3.ack()
		assert.Error(err, "ack should fail with queue not implementing IAcknowledgeable")
		assert.Contains(err.Error(), "not acknowledgeable", "error should mention not acknowledgeable")
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
		assert.Equal(jobData, j.Payload(), "job data should match")
		assert.Equal("Created", j.Status(), "job status should be 'Created'")

		// Save and send a result
		expectedResult := 42
		j.saveAndSendResult(expectedResult)

		// Get the result
		result, err := j.Result()
		assert.Equal(expectedResult, result, "result should match what was sent")
		assert.Nil(err, "error should be nil")
	})

	t.Run("resultJob error handling", func(t *testing.T) {
		// Create a new result job
		j := newResultJob[string, int]("test data", jobConfigs{Id: "result-job-error"})
		assert := assert.New(t)

		// Save and send an error
		expectedErr := errors.New("test error")
		j.saveAndSendError(expectedErr)

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

	t.Run("resultJob Result after saveAndSendError", func(t *testing.T) {
		// Create a new result job
		j := newResultJob[string, int]("test data", jobConfigs{Id: "result-job-error"})
		assert := assert.New(t)

		// Send an error through the job
		expectedErr := errors.New("job closed error")
		j.saveAndSendError(expectedErr)

		// Try to get result
		result, err := j.Result()
		assert.Error(err, "Result should return an error after saveAndSendError")
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
		assert.Equal(jobData, j.Payload(), "job data should match")
		assert.Equal("Created", j.Status(), "job status should be 'Created'")

		// Send an error
		expectedErr := errors.New("test error")
		j.sendError(expectedErr)

		// Get the error
		err := j.Err()
		assert.Equal(expectedErr, err, "error should match what was sent")
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
