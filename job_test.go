package varmq

// import (
// 	"errors"
// 	"testing"

// 	"github.com/stretchr/testify/assert"
// )

// func TestJob(t *testing.T) {
// 	t.Run("job creation with newJob", func(t *testing.T) {
// 		// Create a new job
// 		jobData := "test data"
// 		jobId := "job-123"
// 		j := newJob[string, int](jobData, jobConfigs{Id: jobId})

// 		assert := assert.New(t)

// 		// Validate job structure
// 		assert.NotNil(j, "job should not be nil")
// 		assert.Equal(jobId, j.ID(), "job ID should match")
// 		assert.Equal(jobData, j.Payload(), "job data should match")
// 		assert.Equal("Created", j.Status(), "job status should be 'Created'")
// 		assert.False(j.IsClosed(), "job should not be closed initially")
// 	})

// 	t.Run("job status transitions", func(t *testing.T) {
// 		// Create a new job
// 		j := newJob[string, int]("test", jobConfigs{Id: "job-status"})
// 		assert := assert.New(t)

// 		// Initial status should be created
// 		assert.Equal("Created", j.Status(), "initial status should be 'Created'")

// 		// Transition to queued
// 		j.changeStatus(queued)
// 		assert.Equal("Queued", j.Status(), "status should be 'Queued' after change")

// 		// Transition to processing
// 		j.changeStatus(processing)
// 		assert.Equal("Processing", j.Status(), "status should be 'Processing' after change")

// 		// Transition to finished
// 		j.changeStatus(finished)
// 		assert.Equal("Finished", j.Status(), "status should be 'Finished' after change")

// 		// Transition to closed
// 		j.changeStatus(closed)
// 		assert.Equal("Closed", j.Status(), "status should be 'Closed' after change")
// 		assert.True(j.IsClosed(), "job should be marked as closed")
// 	})

// 	t.Run("saving and sending results", func(t *testing.T) {
// 		// Create a new job
// 		j := newJob[string]("test data", jobConfigs{Id: "job-result"})
// 		assert := assert.New(t)

// 		// Save and send a result
// 		expectedResult := 42
// 		j.saveAndSendResult(expectedResult)

// 		// Check if result was saved correctly
// 		assert.Equal(expectedResult, j.resultController.result.Data, "result should be saved in Result")
// 		assert.Nil(j.resultController.result.Err, "error should be nil")

// 		// Get the result
// 		result, err := j.Result()
// 		assert.Equal(expectedResult, result, "result should match what was sent")
// 		assert.Nil(err, "error should be nil")
// 	})

// 	t.Run("saving and sending errors", func(t *testing.T) {
// 		// Create a new job
// 		j := newJob[string, int]("test data", jobConfigs{Id: "job-error"})
// 		// Initialize the result controller (this is now needed since it's not done in newJob)
// 		j.withResult(1)
// 		assert := assert.New(t)

// 		// Save and send an error
// 		expectedErr := errors.New("test error")
// 		j.saveAndSendError(expectedErr)

// 		// Check if error was saved correctly
// 		var zeroValue int
// 		assert.Equal(zeroValue, j.resultController.result.Data, "data should be zero value")
// 		assert.Equal(expectedErr, j.resultController.result.Err, "error should be saved in Result")

// 		// Get the result
// 		result, err := j.Result()
// 		assert.Equal(zeroValue, result, "result should be zero value")
// 		assert.Equal(expectedErr, err, "error should match what was sent")
// 	})

// 	t.Run("job JSON serialization", func(t *testing.T) {
// 		// Create a new job with a result
// 		j := newJob[string, int]("test data", jobConfigs{Id: "job-json"})
// 		// Initialize the result controller
// 		j.withResult(1)
// 		j.saveAndSendResult(42)

// 		assert := assert.New(t)

// 		// Serialize to JSON
// 		jsonData, err := j.Json()
// 		assert.Nil(err, "JSON serialization should not fail")
// 		assert.NotEmpty(jsonData, "JSON data should not be empty")

// 		// We can't fully test parsing here as it's not exported,
// 		// but we can verify the JSON contains expected fields
// 		jsonStr := string(jsonData)
// 		assert.Contains(jsonStr, `"id":"job-json"`, "JSON should contain job ID")
// 		assert.Contains(jsonStr, `"input":"test data"`, "JSON should contain job input")
// 		assert.Contains(jsonStr, `"status":"Created"`, "JSON should contain job status")
// 		assert.Contains(jsonStr, `"Data":42`, "JSON should contain result data")
// 	})

// 	t.Run("closing a job", func(t *testing.T) {
// 		// Create a new job
// 		j := newJob[string, int]("test data", jobConfigs{Id: "job-close"})
// 		assert := assert.New(t)

// 		// Close the job
// 		err := j.close()
// 		assert.Nil(err, "closing job should not fail")
// 		assert.Equal("Closed", j.Status(), "job status should be 'Closed' after close")
// 		assert.True(j.IsClosed(), "job should be marked as closed")

// 		// Attempting to close again should fail
// 		err = j.close()
// 		assert.NotNil(err, "closing an already closed job should fail")
// 		assert.Contains(err.Error(), "already closed", "error message should indicate job is already closed")
// 	})
// }
