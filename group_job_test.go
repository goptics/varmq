package varmq

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGroupJob(t *testing.T) {
	t.Run("group job creation with newGroupJob", func(t *testing.T) {
		// Create a new group job with buffer size 3
		bufferSize := 3
		gj := newGroupJob[string, int](bufferSize)

		assert := assert.New(t)

		// Validate group job structure
		assert.NotNil(gj, "group job should not be nil")
		assert.NotNil(gj.job, "underlying job should not be nil")
		assert.NotNil(gj.wg, "wait group should not be nil")
		assert.NotNil(gj.resultChannel, "result channel should be initialized")
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
		gj := newGroupJob[string, int](bufferSize)

		// Create a job within the group
		jobData := "test group data"
		jobId := "group-job-123"
		newJob := gj.NewJob(jobData, jobConfigs{Id: jobId})

		assert := assert.New(t)

		// Validate the new job
		assert.NotNil(newJob, "new job in group should not be nil")
		assert.Equal(generateGroupId(jobId), newJob.ID(), "job ID should have group prefix")
		assert.Equal(jobData, newJob.Data(), "job data should match")
		assert.Equal(gj.resultChannel, newJob.resultChannel, "result channel should be shared with the group")
		assert.Equal(gj.wg, newJob.wg, "wait group should be shared with the group")
	})

	t.Run("closing a group job", func(t *testing.T) {
		// Create a group job with small buffer to avoid wait group blocking
		bufferSize := 1
		gj := newGroupJob[string, int](bufferSize)

		// Manually reduce the wait group count to match our single Close() call
		// This prevents the wait group from blocking indefinitely
		gj.wg.Add(-bufferSize + 1)

		assert := assert.New(t)

		// Close the job
		err := gj.close()
		assert.Nil(err, "closing group job should not fail")
		assert.Equal("Closed", gj.Status(), "job status should be 'Closed' after close")
		assert.True(gj.IsClosed(), "job should be marked as closed")

		// Attempting to close again should fail
		err = gj.close()
		assert.NotNil(err, "closing an already closed job should fail")
		assert.Contains(err.Error(), "already closed", "error message should indicate job is already closed")
	})

	t.Run("job status transitions in a group job", func(t *testing.T) {
		// Create a new group job
		bufferSize := 1
		gj := newGroupJob[string, int](bufferSize)

		// Reduce wait group count to prevent deadlock when testing
		gj.wg.Add(-int(bufferSize))

		assert := assert.New(t)

		// Initial status should be created (the job.status.Load() is defaulted to 0)
		assert.Equal("Created", gj.Status(), "initial status should be 'Created'")

		// Transition to queued
		gj.ChangeStatus(queued)
		assert.Equal("Queued", gj.Status(), "status should be 'Queued' after change")

		// Transition to processing
		gj.ChangeStatus(processing)
		assert.Equal("Processing", gj.Status(), "status should be 'Processing' after change")

		// Attempting to close a processing job should fail
		err := gj.close()
		assert.NotNil(err, "closing a processing job should fail")
		assert.Contains(err.Error(), "processing", "error message should indicate job is processing")

		// Transition to finished
		gj.ChangeStatus(finished)
		assert.Equal("Finished", gj.Status(), "status should be 'Finished' after change")
	})
}
