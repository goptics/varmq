package void_queue

import (
	"testing"
	"time"

	"github.com/fahimfaisaal/gocq/internal/common"
	"github.com/fahimfaisaal/gocq/types"
)

func TestConcurrentVoidQueue(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		q := NewQueue(2, func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.WaitAndClose()

		q.Add(5)
	})

	t.Run("AddAll", func(t *testing.T) {
		q := NewQueue(2, func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.WaitAndClose()

		q.AddAll([]int{1, 2, 3, 4, 5})
	})

	t.Run("PauseAndResume", func(t *testing.T) {
		q := NewQueue(2, func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.Close()

		q.Add(1)
		q.Add(2)
		q.Pause()
		q.Add(3)

		time.Sleep(50 * time.Millisecond)
		if count := q.PendingCount(); count != 1 {
			t.Errorf("Expected pending count to be 1, got %d", count)
		}

		q.Resume()

		q.WaitUntilFinished()
		if count := q.PendingCount(); count != 0 {
			t.Errorf("Expected pending count to be 0, got %d", count)
		}
		if count := q.CurrentProcessingCount(); count != 0 {
			t.Errorf("Expected current processing count to be 0, got %d", count)
		}
	})
}

func TestConcurrentVoidPriorityQueue(t *testing.T) {
	t.Run("Add with Priority", func(t *testing.T) {
		q := NewPriorityQueue(1, func(data int) error {
			common.Double(data)
			return nil
		}).Pause()
		defer q.Close()

		q.Add(1, 2)
		q.Add(2, 1)
		q.Add(3, 0)

		if count := q.PendingCount(); count != 3 {
			t.Errorf("Expected pending count to be 3, got %d", count)
		}

		q.Resume()

		q.WaitUntilFinished()
		if count := q.PendingCount(); count != 0 {
			t.Errorf("Expected pending count to be 0, got %d", count)
		}
		if count := q.CurrentProcessingCount(); count != 0 {
			t.Errorf("Expected current processing count to be 0, got %d", count)
		}
	})

	t.Run("AddAll with Priority", func(t *testing.T) {
		q := NewPriorityQueue(2, func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.Close()

		q.AddAll([]types.PQItem[int]{
			{Value: 1, Priority: 2},
			{Value: 2, Priority: 1},
			{Value: 4, Priority: 2},
			{Value: 5, Priority: 1},
			{Value: 3},
		})

		q.WaitUntilFinished()
		if count := q.PendingCount(); count != 0 {
			t.Errorf("Expected pending count to be 0, got %d", count)
		}
		if count := q.CurrentProcessingCount(); count != 0 {
			t.Errorf("Expected current processing count to be 0, got %d", count)
		}
	})
}
