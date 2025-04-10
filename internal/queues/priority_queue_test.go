package queues

import (
	"testing"
)

func TestPriorityQueue(t *testing.T) {
	t.Run("Basic Operations", func(t *testing.T) {
		pq := NewPriorityQueue[int]()

		if len := pq.Len(); len != 0 {
			t.Errorf("expected length 0, got %d", len)
		}

		pq.Enqueue(1, 2)
		pq.Enqueue(2, 1)
		pq.Enqueue(3, 1)

		if len := pq.Len(); len != 3 {
			t.Errorf("expected length 2, got %d", len)
		}

		val, ok := pq.Dequeue()
		if !ok || val != 2 {
			t.Errorf("expected value 2, got %d", val)
		}

		if len := pq.Len(); len != 2 {
			t.Errorf("expected length 2, got %d", len)
		}

		val, ok = pq.Dequeue()
		if !ok || val != 3 {
			t.Errorf("expected value 3, got %d", val)
		}

		val, ok = pq.Dequeue()
		if !ok || val != 1 {
			t.Errorf("expected value 1, got %d", val)
		}

		val, ok = pq.Dequeue()
		if ok || val != 0 {
			t.Errorf("expected no value, got %d", val)
		}
	})

	t.Run("Empty Queue", func(t *testing.T) {
		pq := NewPriorityQueue[int]()
		val, ok := pq.Dequeue()
		if ok || val != 0 {
			t.Errorf("expected no value, got %d", val)
		}
	})

	t.Run("Values Method", func(t *testing.T) {
		pq := NewPriorityQueue[int]()
		pq.Enqueue(1, 2)
		pq.Enqueue(2, 1)

		values := pq.Values()
		expected := []int{2, 1}
		for i, v := range values {
			if v != expected[i] {
				t.Errorf("expected value %d, got %d", expected[i], v)
			}
		}
	})
}
