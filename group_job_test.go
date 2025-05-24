package varmq

// import (
// 	"strings"
// 	"sync"
// 	"testing"
// 	"time"

// 	"github.com/stretchr/testify/assert"
// )

// func TestGroupJob(t *testing.T) {
// 	t.Run("group job creation with newGroupJob", func(t *testing.T) {
// 		// Create a new group job with buffer size 3
// 		bufferSize := 3
// 		gj := newGroupJob[string, int](bufferSize)

// 		assert := assert.New(t)

// 		// Validate group job structure
// 		assert.NotNil(gj, "group job should not be nil")
// 		assert.NotNil(gj.resultController, "result channel should be initialized")
// 		assert.NotNil(gj.done, "done channel should be initialized")
// 		assert.NotNil(gj.len, "atomic counter should be initialized")
// 		assert.Equal(bufferSize, gj.Len(), "initial length should match buffer size")
// 	})

// 	t.Run("generate group ID", func(t *testing.T) {
// 		// Test group ID generation
// 		baseId := "test-job"
// 		groupId := generateGroupId(baseId)

// 		assert := assert.New(t)

// 		// Group ID should have the expected prefix
// 		assert.True(strings.HasPrefix(groupId, groupIdPrefixed), "group ID should have the correct prefix")
// 		assert.Equal(groupIdPrefixed+baseId, groupId, "group ID should be prefix + base ID")
// 	})

// 	t.Run("creating new job in a group", func(t *testing.T) {
// 		// Create a new group job
// 		bufferSize := 3
// 		gj := newGroupJob[string, int](bufferSize)

// 		// Create a job within the group
// 		jobData := "test group data"
// 		jobId := "group-job-123"
// 		newJob := gj.newJob(jobData, jobConfigs{Id: jobId})

// 		assert := assert.New(t)

// 		// Validate the new job
// 		assert.NotNil(newJob, "new job in group should not be nil")
// 		assert.Equal(generateGroupId(jobId), newJob.ID(), "job ID should have group prefix")
// 		assert.Equal(jobData, newJob.Payload(), "job data should match")
// 		assert.Equal(gj.resultController, newJob.resultController, "result channel should be shared with the group")
// 		assert.Equal(gj.done, newJob.done, "done channel should be shared with the group")
// 		assert.Equal(gj.len, newJob.len, "length counter should be shared with the group")
// 	})

// 	t.Run("getting results from a group job", func(t *testing.T) {
// 		// Create a group job
// 		bufferSize := 2
// 		gj := newGroupJob[string, int](bufferSize)

// 		assert := assert.New(t)

// 		// Add a result to the channel
// 		result := Result[int]{Data: 42, Err: nil}
// 		gj.resultController.Send(result)

// 		// Get results
// 		ch, err := gj.Results()
// 		assert.Nil(err, "getting results should not fail")
// 		assert.NotNil(ch, "result channel should not be nil")

// 		// Check if the result can be read
// 		select {
// 		case r := <-ch:
// 			assert.Equal(result, r, "result value should match")
// 		case <-time.After(time.Millisecond * 100):
// 			t.Fatal("timeout waiting for result")
// 		}
// 	})

// 	t.Run("wait for group job completion", func(t *testing.T) {
// 		// Create a group job
// 		bufferSize := 1
// 		gj := newGroupJob[string, int](bufferSize)

// 		// Setup done channel to simulate completion
// 		go func() {
// 			time.Sleep(time.Millisecond * 50)
// 			close(gj.done)
// 		}()

// 		// Create a wait group to synchronize test completion
// 		var wg sync.WaitGroup
// 		wg.Add(1)

// 		// Start waiting for job completion in a goroutine
// 		go func() {
// 			defer wg.Done()
// 			gj.Wait()
// 		}()

// 		// Wait with timeout
// 		waitCh := make(chan struct{})
// 		go func() {
// 			wg.Wait()
// 			close(waitCh)
// 		}()

// 		select {
// 		case <-waitCh:
// 			// Success, wait completed
// 		case <-time.After(time.Millisecond * 500):
// 			t.Fatal("timeout waiting for job completion")
// 		}
// 	})

// 	t.Run("getting length of a group job", func(t *testing.T) {
// 		// Create a group job with buffer size 5
// 		bufferSize := 5
// 		gj := newGroupJob[string, int](bufferSize)

// 		assert := assert.New(t)

// 		// Verify initial length
// 		assert.Equal(bufferSize, gj.Len(), "initial length should match buffer size")

// 		// Simulate jobs completing by decrementing counter
// 		gj.len.Add(^uint32(0)) // Subtract 1
// 		assert.Equal(bufferSize-1, gj.Len(), "length should be decremented after a job completes")
// 	})

// 	t.Run("draining a group job", func(t *testing.T) {
// 		// Create a group job
// 		bufferSize := 3
// 		gj := newGroupJob[string, int](bufferSize)

// 		assert := assert.New(t)

// 		// Add results to drain
// 		gj.resultController.Send(Result[int]{Data: 1, Err: nil})
// 		gj.resultController.Send(Result[int]{Data: 2, Err: nil})

// 		// Drain the results
// 		err := gj.Drain()
// 		assert.Nil(err, "draining should not fail")

// 		// After draining, we should be able to add more results
// 		// (This verifies the drain is working and not blocking)
// 		gj.resultController.Send(Result[int]{Data: 3, Err: nil})
// 	})

// 	t.Run("closing a group job", func(t *testing.T) {
// 		// Create a group job with small buffer to avoid wait group blocking
// 		bufferSize := 2
// 		gj := newGroupJob[string, int](bufferSize)

// 		assert := assert.New(t)

// 		// Close the job - this should decrement the counter but not close channels yet
// 		err := gj.close()
// 		assert.Nil(err, "closing group job should not fail")
// 		assert.Equal("Closed", gj.Status(), "job status should be 'Closed' after close")
// 		assert.True(gj.IsClosed(), "job should be marked as closed")
// 		assert.Equal(bufferSize-1, gj.Len(), "length should be decremented after close")

// 		// Close another job from the group
// 		jobData := "test data"
// 		jobId := "test-job"
// 		newJob := gj.newJob(jobData, jobConfigs{Id: jobId})
// 		err = newJob.close()
// 		assert.Nil(err, "closing second job should not fail")
// 		assert.Equal(bufferSize-2, gj.Len(), "length should be decremented again")

// 		// Attempting to close again should fail
// 		err = gj.close()
// 		assert.NotNil(err, "closing an already closed job should fail")
// 		assert.Contains(err.Error(), "already closed", "error message should indicate job is already closed")
// 	})

// 	t.Run("closing all jobs in a group", func(t *testing.T) {
// 		// Create a group job with buffer size 2
// 		bufferSize := 2
// 		gj := newGroupJob[string, int](bufferSize)

// 		// Create a second job in the group
// 		job2 := gj.newJob("test data", jobConfigs{Id: "test-job"})

// 		assert := assert.New(t)

// 		// Close both jobs, which should close the group
// 		_ = gj.close()
// 		_ = job2.close()

// 		// Length should be 0 now
// 		assert.Equal(0, gj.Len(), "length should be 0 after all jobs are closed")

// 		// Check if the channels are closed by trying to add a result
// 		// This should panic, so we recover from it
// 		didPanic := false
// 		func() {
// 			defer func() {
// 				if r := recover(); r != nil {
// 					didPanic = true
// 				}
// 			}()
// 			gj.resultController.Send(Result[int]{Data: 1, Err: nil})
// 		}()
// 		assert.True(didPanic, "adding to a closed channel should panic")
// 	})

// 	t.Run("job status transitions in a group job", func(t *testing.T) {
// 		// Create a new group job
// 		bufferSize := 1
// 		gj := newGroupJob[string, int](bufferSize)

// 		assert := assert.New(t)

// 		// Initial status should be created (the job.status.Load() is defaulted to 0)
// 		assert.Equal("Created", gj.Status(), "initial status should be 'Created'")

// 		// Transition to queued
// 		gj.changeStatus(queued)
// 		assert.Equal("Queued", gj.Status(), "status should be 'Queued' after change")

// 		// Transition to processing
// 		gj.changeStatus(processing)
// 		assert.Equal("Processing", gj.Status(), "status should be 'Processing' after change")

// 		// Attempting to close a processing job should fail
// 		err := gj.close()
// 		assert.NotNil(err, "closing a processing job should fail")
// 		assert.Contains(err.Error(), "processing", "error message should indicate job is processing")

// 		// Transition to finished
// 		gj.changeStatus(finished)
// 		assert.Equal("Finished", gj.Status(), "status should be 'Finished' after change")
// 	})
// }
