package varmq

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
		assert.Empty(generateGroupId(""), "a member without an id has none, like a single job")
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
		assert.Equal(rgj.resp, newJob.resp, "Response should be shared with the group")
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
		assert.Equal(egj.resp, newJob.resp, "Response should be shared with the group")
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

func TestGroupJobSendAndDrain(t *testing.T) {
	t.Run("result group member sends results and errors into the shared channel", func(t *testing.T) {
		rgj := newResultGroupJob[string, int](2)
		a := rgj.newJob("a", jobConfigs{Id: "1"})
		b := rgj.newJob("b", jobConfigs{Id: "2"})

		a.sendResult(42)
		b.sendError(errors.New("boom"))

		got := []Result[int]{<-rgj.Results(), <-rgj.Results()}
		assert.Equal(t, Result[int]{JobId: "g:1", Data: 42}, got[0])
		assert.Equal(t, "g:2", got[1].JobId)
		assert.EqualError(t, got[1].Err, "boom")
	})

	t.Run("Drain empties a result group channel after it is closed", func(t *testing.T) {
		rgj := newResultGroupJob[string, int](1)
		j := rgj.newJob("a", jobConfigs{})
		j.sendResult(1)
		assert.NoError(t, j.Close()) // last member: closes the channel

		rgj.Drain()
		assert.Eventually(t, func() bool {
			_, ok := <-rgj.Results()
			return !ok
		}, time.Second, time.Millisecond, "channel should be drained and closed")
	})

	t.Run("Drain empties an error group channel after it is closed", func(t *testing.T) {
		egj := newErrorGroupJob[string](1)
		j := egj.newJob("a", jobConfigs{})
		j.sendError(errors.New("boom"))
		assert.NoError(t, j.Close())

		egj.Drain()
		assert.Eventually(t, func() bool {
			_, ok := <-egj.Errs()
			return !ok
		}, time.Second, time.Millisecond, "channel should be drained and closed")
	})
}

// #134: every job of a group sends its result or error on one shared
// Response, from as many goroutines as the worker runs; run with -race.
func TestGroupJobConcurrentSends(t *testing.T) {
	items := make([]Item[int], 2000)
	for i := range items {
		items[i] = Item[int]{Data: i}
	}

	t.Run("results", func(t *testing.T) {
		w := NewResultWorker(func(j Job[int]) (int, error) { return j.Data(), nil }, 8)
		defer w.Stop()

		n := 0
		for range w.BindQueue().AddAll(items).Results() {
			n++
		}
		assert.Equal(t, len(items), n)
	})

	t.Run("errors", func(t *testing.T) {
		w := NewErrWorker(func(j Job[int]) error { return errors.New("fail") }, 8)
		defer w.Stop()

		n := 0
		for range w.BindQueue().AddAll(items).Errs() {
			n++
		}
		assert.Equal(t, len(items), n)
	})
}

// #155: an empty AddAll group has no member whose Close would close its
// channel, so ranging over Results or Errs must still end at once.
func TestAddAllWithNoItemsClosesChannel(t *testing.T) {
	rw := NewResultWorker(func(j Job[int]) (int, error) { return j.Data(), nil }, 2)
	defer rw.Stop()
	ew := NewErrWorker(func(j Job[int]) error { return errors.New("fail") }, 2)
	defer ew.Stop()

	for _, items := range [][]Item[int]{nil, {}} {
		assert.True(t, isClosed(rw.BindQueue().AddAll(items).Results()), "queue Results, items %#v", items)
		assert.True(t, isClosed(rw.BindPriorityQueue().AddAll(items).Results()), "priority Results, items %#v", items)
		assert.True(t, isClosed(ew.BindQueue().AddAll(items).Errs()), "queue Errs, items %#v", items)
		assert.True(t, isClosed(ew.BindPriorityQueue().AddAll(items).Errs()), "priority Errs, items %#v", items)
	}
}

// isClosed reports whether ch is closed and empty, without blocking.
func isClosed[V any](ch <-chan V) bool {
	select {
	case _, ok := <-ch:
		return !ok
	default:
		return false
	}
}
