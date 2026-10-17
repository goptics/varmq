package queues

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeapQueue(t *testing.T) {
	t.Run("Len", func(t *testing.T) {
		assert := assert.New(t)
		hq := &heapQueue[int]{}

		assert.Equal(0, hq.Len(), "Empty queue should have length 0")

		hq.push(enqItem[int]{Value: 1, Priority: 1})
		assert.Equal(1, hq.Len(), "Queue with one item should have length 1")
	})

	t.Run("less", func(t *testing.T) {
		assert := assert.New(t)

		t.Run("Different priorities", func(t *testing.T) {
			hq := &heapQueue[int]{items: []enqItem[int]{
				{Value: 1, Priority: 2},
				{Value: 2, Priority: 1},
			}}

			assert.True(hq.less(1, 0), "Item with lower priority (1) should be less than item with higher priority (2)")
		})

		t.Run("Same priorities with different indices", func(t *testing.T) {
			hq := &heapQueue[int]{items: []enqItem[int]{
				{Value: 1, Priority: 1, Index: 1},
				{Value: 2, Priority: 1, Index: 0},
			}}

			assert.True(hq.less(1, 0), "With same priority, item with lower index (0) should be less than item with higher index (1)")
		})
	})

	t.Run("Push Pop Order", func(t *testing.T) {
		assert := assert.New(t)
		hq := &heapQueue[string]{}

		type testItem struct {
			value    string
			priority int
			index    int
		}

		items := []testItem{
			{"task1", 5, 0},
			{"task2", 2, 1},
			{"task3", 1, 2},
			{"task4", 3, 3},
			{"task5", 1, 4}, // Same priority as task3, but higher index
		}

		for _, i := range items {
			hq.push(enqItem[string]{
				Value:    i.value,
				Priority: i.priority,
				Index:    i.index,
			})
		}

		// Verify heap property by popping all items
		expected := []string{"task3", "task5", "task2", "task4", "task1"}
		for i, exp := range expected {
			item := hq.pop()
			assert.Equal(exp, item.Value, "Item popped from heap should match expected value at index %d", i)
		}

		assert.Equal(0, hq.Len(), "Heap should be empty after popping all items")
	})

	t.Run("Mixed Push And Pop", func(t *testing.T) {
		assert := assert.New(t)
		hq := &heapQueue[int]{}

		// Push in non-priority order
		hq.push(enqItem[int]{Value: 10, Priority: 10, Index: 0})
		hq.push(enqItem[int]{Value: 5, Priority: 5, Index: 1})
		hq.push(enqItem[int]{Value: 15, Priority: 15, Index: 2})

		// Verify the minimum is correct
		assert.Equal(5, hq.pop().Value, "First pop should return minimum priority item")

		// Push more items
		hq.push(enqItem[int]{Value: 2, Priority: 2, Index: 3})
		hq.push(enqItem[int]{Value: 7, Priority: 7, Index: 4})

		// Mix push and pop
		assert.Equal(2, hq.pop().Value, "Second pop should return new minimum priority item")

		hq.push(enqItem[int]{Value: 1, Priority: 1, Index: 5})

		// Verify the heap property by popping in expected order
		expected := []int{1, 7, 10, 15}
		for i, exp := range expected {
			assert.Equal(exp, hq.pop().Value, "Item at position %d should have expected value", i)
		}
	})

	t.Run("Priority Tiebreaker", func(t *testing.T) {
		assert := assert.New(t)
		hq := &heapQueue[string]{}

		// Add items with same priority but different indices
		hq.push(enqItem[string]{Value: "first", Priority: 1, Index: 1})
		hq.push(enqItem[string]{Value: "second", Priority: 1, Index: 0})

		// The item with lower index should be popped first
		assert.Equal("second", hq.pop().Value, "Item with lower index should be popped first")
		assert.Equal("first", hq.pop().Value, "Item with higher index should be popped second")
	})

	t.Run("Pop clears the vacated slot", func(t *testing.T) {
		assert := assert.New(t)
		hq := &heapQueue[*int]{}
		v := 1
		hq.push(enqItem[*int]{Value: &v, Priority: 1})

		assert.Same(&v, hq.pop().Value)
		assert.Nil(hq.items[:1][0].Value, "popped slot in the backing array should not keep the value alive")
	})
}
