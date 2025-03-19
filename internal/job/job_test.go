package job

import (
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

		if status := job.Status(); status != "Created" {
			t.Errorf("expected Created, got %s", status)
		}

		job.ChangeStatus(Queued)

		if status := job.Status(); status != "Queued" {
			t.Errorf("expected Queued, got %s", status)
		}

		job.ChangeStatus(Processing)

		if status := job.Status(); status != "Processing" {
			t.Errorf("expected Processing, got %s", status)
		}

		job.ChangeStatus(Finished)

		if status := job.Status(); status != "Finished" {
			t.Errorf("expected Finished, got %s", status)
		}

		job.ChangeStatus(Closed)

		if status := job.Status(); status != "Closed" {
			t.Errorf("expected Closed, got %s", status)
		}
	})

	t.Run("Result", func(t *testing.T) {
		job := New[int, int](0)

		go func() {
			time.Sleep(100 * time.Millisecond)
			job.SendResult(42)
		}()

		result, err := job.Result()
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result != 42 {
			t.Errorf("expected 42, got %d", result)
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

	t.Run("CloseProcessing", func(t *testing.T) {
		job := New[int, int](0).ChangeStatus(Processing)

		err := job.Close()
		if err == nil {
			t.Errorf("expected job is processing error, got %v", err)
		}
	})
}
