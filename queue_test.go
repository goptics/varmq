package gocq

import (
	"testing"
	"time"
)

// TestConcurrentPriorityQueue tests the functionality of ConcurrentPriorityQueue.
func TestConcurrentPriorityQueue(t *testing.T) {
	t.Run("Add with Priority", func(t *testing.T) {
		worker := func(data int) (int, error) {
			return Double(data), nil
		}

		q := NewPriorityQueue(1, worker).Pause()

		job1 := q.Add(1, 2)
		job2 := q.Add(2, 1)
		job3 := q.Add(3, 0)

		if count := q.PendingCount(); count != 3 {
			t.Errorf("Expected pending count to be 3, got %d", count)
		}

		q.Resume()
		if result, _ := job3.WaitForResult(); result != 6 {
			t.Errorf("Expected result to be 6, got %d", result)
		}
		if result, _ := job2.WaitForResult(); result != 4 {
			t.Errorf("Expected result to be 4, got %d", result)
		}
		if result, _ := job1.WaitForResult(); result != 2 {
			t.Errorf("Expected result to be 2, got %d", result)
		}

		q.WaitUntilFinished()
		if count := q.PendingCount(); count != 0 {
			t.Errorf("Expected pending count to be 0, got %d", count)
		}
		if count := q.CurrentProcessingCount(); count != 0 {
			t.Errorf("Expected current processing count to be 0, got %d", count)
		}
	})
}

// TestConcurrentQueue tests the functionality of ConcurrentQueue.
func TestConcurrentQueue(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		t.Parallel()
		q := NewQueue(2, func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.Close()

		result, _ := q.Add(5).WaitForResult()

		if result != 10 {
			t.Errorf("Expected result to be 10, got %d", result)
		}
	})

	t.Run("WaitUntilFinished", func(t *testing.T) {
		q := NewQueue(2, func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.Close()

		q.Add(1)
		q.Add(2)
		q.Add(3)

		q.WaitUntilFinished()

		if q.PendingCount() != 0 {
			t.Errorf("Expected pending count to be 0, got %d", q.PendingCount())
		}
		if q.CurrentProcessingCount() != 0 {
			t.Errorf("Expected current processing count to be 0, got %d", q.CurrentProcessingCount())
		}
	})

	t.Run("Concurrency", func(t *testing.T) {
		t.Parallel()
		concurrency := uint32(2)
		q := NewQueue(concurrency, func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.Close()

		// Add more Jobs than concurrency
		for i := 0; i < 5; i++ {
			q.Add(i)
		}

		// WaitForResult a bit to let some processing happen
		time.Sleep(150 * time.Millisecond)

		// Check if only concurrency number of Jobs are being processed
		if current := q.CurrentProcessingCount(); current > concurrency {
			t.Errorf("Processing more Jobs than concurrency allows. Got %d, want <= %d", current, concurrency)
		}
	})

	t.Run("WaitAndClose", func(t *testing.T) {
		q := NewQueue(2, func(data int) (int, error) {
			return Double(data), nil
		})

		q.Add(1)
		q.Add(2)
		q.Add(3)

		q.WaitAndClose()

		if q.PendingCount() != 0 {
			t.Errorf("Expected pending count to be 0, got %d", q.PendingCount())
		}
		if q.CurrentProcessingCount() != 0 {
			t.Errorf("Expected current processing count to be 0, got %d", q.CurrentProcessingCount())
		}
	})

	t.Run("PauseAndResume", func(t *testing.T) {
		worker := func(data int) (int, error) {
			return Double(data), nil
		}

		q := NewQueue(2, worker)

		job1 := q.Add(1)
		job2 := q.Add(2)

		if result, _ := job1.WaitForResult(); result != 2 {
			t.Errorf("Expected result to be 2, got %d", result)
		}
		if result, _ := job2.WaitForResult(); result != 4 {
			t.Errorf("Expected result to be 4, got %d", result)
		}

		q.Pause()
		job3 := q.Add(3)
		time.Sleep(50 * time.Millisecond)
		if count := q.PendingCount(); count != 1 {
			t.Errorf("Expected pending count to be 1, got %d", count)
		}

		q.Resume()
		if result, _ := job3.WaitForResult(); result != 6 {
			t.Errorf("Expected result to be 6, got %d", result)
		}

		q.WaitUntilFinished()
		if count := q.PendingCount(); count != 0 {
			t.Errorf("Expected pending count to be 0, got %d", count)
		}
		if count := q.CurrentProcessingCount(); count != 0 {
			t.Errorf("Expected current processing count to be 0, got %d", count)
		}
	})
}
