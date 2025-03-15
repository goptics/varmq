package job

import (
	"errors"
	"testing"
	"time"
)

func TestJob(t *testing.T) {
	t.Run("Status", func(t *testing.T) {
		job := New[int, int](0)

		if job.Status() != "Created" {
			t.Errorf("expected Created, got %s", job.Status())
		}

		job.ChangeStatus(Queued)

		if job.Status() != "Queued" {
			t.Errorf("expected Queued, got %s", job.Status())
		}

		job.ChangeStatus(Processing)

		if job.Status() != "Processing" {
			t.Errorf("expected Processing, got %s", job.Status())
		}

		job.ChangeStatus(Finished)
		if job.Status() != "Finished" {
			t.Errorf("expected Finished, got %s", job.Status())
		}

		job.ChangeStatus(Closed)
		if job.Status() != "Closed" {
			t.Errorf("expected Closed, got %s", job.Status())
		}
	})

	t.Run("IsClosed", func(t *testing.T) {
		job := New[int, int](0)

		if job.IsClosed() {
			t.Errorf("expected false, got true")
		}

		job.ChangeStatus(Closed)
		if !job.IsClosed() {
			t.Errorf("expected true, got false")
		}
	})

	t.Run("ChangeStatus", func(t *testing.T) {
		job := New[int, int](0)

		if status := job.status.Load(); status != Created {
			t.Errorf("expected Created, got %d", status)
		}

		job.ChangeStatus(Queued)

		if status := job.status.Load(); status != Queued {
			t.Errorf("expected Queued, got %d", status)
		}

		job.ChangeStatus(Processing)

		if status := job.status.Load(); status != Processing {
			t.Errorf("expected Processing, got %d", status)
		}

		job.ChangeStatus(Finished)

		if status := job.status.Load(); status != Finished {
			t.Errorf("expected Finished, got %d", status)
		}

		job.ChangeStatus(Closed)

		if status := job.status.Load(); status != Closed {
			t.Errorf("expected Closed, got %d", status)
		}
	})

	t.Run("WaitForResult", func(t *testing.T) {
		job := New[int, int](0)

		go func() {
			time.Sleep(100 * time.Millisecond)
			job.ResultChannel.Data <- 42
		}()

		result, err := job.WaitForResult()
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result != 42 {
			t.Errorf("expected 42, got %d", result)
		}
	})

	t.Run("WaitForError", func(t *testing.T) {
		channel := NewVoidResultChannel()
		job := NewWithResultChannel[int, any](channel)

		go func() {
			time.Sleep(100 * time.Millisecond)
			job.ResultChannel.Err <- errors.New("test error")
		}()

		err := job.WaitForError()
		if err == nil || err.Error() != "test error" {
			t.Errorf("expected test error, got %v", err)
		}
	})

	t.Run("Drain", func(t *testing.T) {
		job := New[int, int](0)

		job.ResultChannel.Data <- 42
		job.ResultChannel.Err <- errors.New("test error")

		job.Drain()

		select {
		case <-job.ResultChannel.Data:
		case <-time.After(5 * time.Second):
			t.Fatal("Data channel not drained")
		}

		select {
		case <-job.ResultChannel.Err:
		case <-time.After(5 * time.Second):
			t.Fatal("Error channel not drained")
		}
	})

	t.Run("Close", func(t *testing.T) {
		job := New[int, int](0)

		err := job.Close()
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if job.Status() != "Closed" {
			t.Errorf("expected Closed, got %s", job.Status())
		}

		err = job.Close()
		if err == nil || err.Error() != "job is already closed" {
			t.Errorf("expected job is already closed, got %v", err)
		}
	})

	t.Run("CloseLocked", func(t *testing.T) {
		job := New[int, int](0).Lock()

		err := job.Close()
		if err == nil || err.Error() != "job is not closeable due to lock" {
			t.Errorf("expected job is not closeable due to lock, got %v", err)
		}
	})

	t.Run("CloseProcessing", func(t *testing.T) {
		job := New[int, int](0).ChangeStatus(Processing)

		err := job.Close()
		if err == nil || err.Error() != "job is processing" {
			t.Errorf("expected job is processing, got %v", err)
		}
	})
}
