package gocq

import (
	"testing"
	"time"
)

// TestConcurrentPriorityQueue tests the functionality of ConcurrentPriorityQueue.
func TestConcurrentPriorityQueue(t *testing.T) {
	t.Run("Add with Priority", func(t *testing.T) {
		worker := func(data int) int {
			return TwoTimes(data)
		}

		q := NewPriorityQueue(1, worker).Pause()

		resp1 := q.Add(1, 2)
		resp2 := q.Add(2, 1)
		resp3 := q.Add(3, 0)

		if count := q.PendingCount(); count != 3 {
			t.Errorf("Expected pending count to be 3, got %d", count)
		}

		q.Resume()
		if result := <-resp3; result != 6 {
			t.Errorf("Expected result to be 6, got %d", result)
		}
		if result := <-resp2; result != 4 {
			t.Errorf("Expected result to be 4, got %d", result)
		}
		if result := <-resp1; result != 2 {
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

	t.Run("AddAll with Priority", func(t *testing.T) {
		worker := func(data int) int {
			return TwoTimes(data)
		}

		q := NewPriorityQueue(2, worker)

		resultChan := q.AddAll([]PQItem[int]{
			{Value: 1, Priority: 2},
			{Value: 2, Priority: 1},
			{Value: 3},
			{Value: 4, Priority: 2},
			{Value: 5, Priority: 1},
		})

		results := make([]int, 0)
		for result := range resultChan {
			results = append(results, result)
		}

		if len(results) != 5 {
			t.Errorf("Expected 5 results, got %d", len(results))
		}

		expectedSum := 30 // sum of [2,4,6,8,10]
		actualSum := 0
		for _, r := range results {
			actualSum += r
		}

		if actualSum != expectedSum {
			t.Errorf("Expected sum of results to be %d, got %d", expectedSum, actualSum)
		}
	})
}

// TestConcurrentQueue tests the functionality of ConcurrentQueue.
func TestConcurrentQueue(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		t.Parallel()
		q := NewQueue(2, func(data int) int {
			return TwoTimes(data)
		})
		defer q.Close()

		resultChan := q.Add(5)
		result := <-resultChan

		if result != 10 {
			t.Errorf("Expected result to be 10, got %d", result)
		}
	})

	t.Run("AddAll", func(t *testing.T) {
		q := NewQueue(2, func(data int) int {
			return TwoTimes(data)
		})
		defer q.Close()

		data := []int{1, 2, 3, 4, 5}
		resultChan := q.AddAll(data)

		results := make([]int, 0)
		for result := range resultChan {
			results = append(results, result)
		}

		if len(results) != 5 {
			t.Errorf("Expected 5 results, got %d", len(results))
		}

		expectedSum := 30 // sum of [2,4,6,8,10]
		actualSum := 0
		for _, r := range results {
			actualSum += r
		}

		if actualSum != expectedSum {
			t.Errorf("Expected sum of results to be %d, got %d", expectedSum, actualSum)
		}
	})

	t.Run("WaitUntilFinished", func(t *testing.T) {
		q := NewQueue(2, func(data int) int {
			return TwoTimes(data)
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
		concurrency := uint(2)
		processed := 0
		q := NewQueue(concurrency, func(data int) int {
			processed++
			return TwoTimes(data)
		})
		defer q.Close()

		// Add more Jobs than concurrency
		for i := 0; i < 5; i++ {
			q.Add(i)
		}

		// Wait a bit to let some processing happen
		time.Sleep(150 * time.Millisecond)

		// Check if only concurrency number of Jobs are being processed
		if current := q.CurrentProcessingCount(); current > concurrency {
			t.Errorf("Processing more Jobs than concurrency allows. Got %d, want <= %d", current, concurrency)
		}
	})

	t.Run("WaitAndClose", func(t *testing.T) {
		q := NewQueue(2, func(data int) int {
			return TwoTimes(data)
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
		worker := func(data int) int {
			return TwoTimes(data)
		}

		q := NewQueue(2, worker)

		resp1 := q.Add(1)
		resp2 := q.Add(2)

		if result := <-resp1; result != 2 {
			t.Errorf("Expected result to be 2, got %d", result)
		}
		if result := <-resp2; result != 4 {
			t.Errorf("Expected result to be 4, got %d", result)
		}

		q.Pause()
		resp3 := q.Add(3)
		time.Sleep(50 * time.Millisecond)
		if count := q.PendingCount(); count != 1 {
			t.Errorf("Expected pending count to be 1, got %d", count)
		}

		q.Resume()
		if result := <-resp3; result != 6 {
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
