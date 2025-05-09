package queues

import (
	"container/heap"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeapQueueLen(t *testing.T) {
	assert := assert.New(t)
	hq := &heapQueue[int]{
		items: make([]*enqItem[int], 0),
	}

	assert.Equal(0, hq.Len(), "Empty queue should have length 0")

	hq.items = append(hq.items, &enqItem[int]{Value: 1, Priority: 1})
	assert.Equal(1, hq.Len(), "Queue with one item should have length 1")
}

func TestHeapQueueLess(t *testing.T) {
	assert := assert.New(t)
	hq := &heapQueue[int]{
		items: make([]*enqItem[int], 0),
	}

	// Test different priorities
	hq.items = append(hq.items, &enqItem[int]{Value: 1, Priority: 2})
	hq.items = append(hq.items, &enqItem[int]{Value: 2, Priority: 1})

	assert.True(hq.Less(1, 0), "Item with lower priority (1) should be less than item with higher priority (2)")

	// Test same priorities, different insertion indices
	hq = &heapQueue[int]{
		items: make([]*enqItem[int], 0),
	}
	hq.items = append(hq.items, &enqItem[int]{Value: 1, Priority: 1, Index: 1})
	hq.items = append(hq.items, &enqItem[int]{Value: 2, Priority: 1, Index: 0})

	assert.True(hq.Less(1, 0), "With same priority, item with lower index (0) should be less than item with higher index (1)")
}

func TestHeapQueueSwap(t *testing.T) {
	assert := assert.New(t)
	hq := &heapQueue[int]{
		items: make([]*enqItem[int], 0),
	}

	item1 := &enqItem[int]{Value: 1, Priority: 1}
	item2 := &enqItem[int]{Value: 2, Priority: 2}

	hq.items = append(hq.items, item1)
	hq.items = append(hq.items, item2)

	hq.Swap(0, 1)

	assert.Equal(item2, hq.items[0], "First item should be swapped")
	assert.Equal(item1, hq.items[1], "Second item should be swapped")
}

func TestHeapQueuePush(t *testing.T) {
	assert := assert.New(t)
	hq := &heapQueue[int]{
		items: make([]*enqItem[int], 0),
	}

	item := &enqItem[int]{Value: 1, Priority: 1}
	hq.Push(item)

	assert.Equal(1, len(hq.items), "Queue should have length 1 after push")
	assert.Equal(item, hq.items[0], "Pushed item should be added to the queue correctly")
}

func TestHeapQueuePop(t *testing.T) {
	assert := assert.New(t)
	hq := &heapQueue[int]{
		items: make([]*enqItem[int], 0),
	}

	item := &enqItem[int]{Value: 1, Priority: 1}
	hq.items = append(hq.items, item)

	popped := hq.Pop()

	assert.Equal(0, len(hq.items), "Queue should be empty after pop")
	assert.Equal(item, popped, "Popped item should match the expected item")
}

func TestHeapQueueWithContainerHeap(t *testing.T) {
	assert := assert.New(t)
	// Test that the heapQueue implementation works correctly with container/heap
	hq := &heapQueue[string]{
		items: make([]*enqItem[string], 0),
	}
	heap.Init(hq)

	// Add items with different priorities
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
		heap.Push(hq, &enqItem[string]{
			Value:    i.value,
			Priority: i.priority,
			Index:    i.index,
		})
	}

	// Verify heap property by popping all items
	expected := []string{"task3", "task5", "task2", "task4", "task1"}
	for i, exp := range expected {
		item := heap.Pop(hq).(*enqItem[string])
		assert.Equal(exp, item.Value, "Item popped from heap should match expected value at index %d", i)
	}

	assert.Equal(0, hq.Len(), "Heap should be empty after popping all items")
}

func TestHeapQueueConcurrentOperations(t *testing.T) {
	assert := assert.New(t)
	// This just tests that the heap operations maintain their property
	// with multiple operations in sequence
	hq := &heapQueue[int]{
		items: make([]*enqItem[int], 0),
	}
	heap.Init(hq)

	// Push in non-priority order
	heap.Push(hq, &enqItem[int]{Value: 10, Priority: 10, Index: 0})
	heap.Push(hq, &enqItem[int]{Value: 5, Priority: 5, Index: 1})
	heap.Push(hq, &enqItem[int]{Value: 15, Priority: 15, Index: 2})

	// Verify the minimum is correct
	min := heap.Pop(hq).(*enqItem[int])
	assert.Equal(5, min.Value, "First pop should return minimum priority item")

	// Push more items
	heap.Push(hq, &enqItem[int]{Value: 2, Priority: 2, Index: 3})
	heap.Push(hq, &enqItem[int]{Value: 7, Priority: 7, Index: 4})

	// Mix push and pop
	min = heap.Pop(hq).(*enqItem[int])
	assert.Equal(2, min.Value, "Second pop should return new minimum priority item")

	heap.Push(hq, &enqItem[int]{Value: 1, Priority: 1, Index: 5})
	
	// Verify the heap property by popping in expected order
	expected := []int{1, 7, 10, 15}
	for i, exp := range expected {
		item := heap.Pop(hq).(*enqItem[int])
		assert.Equal(exp, item.Value, "Item at position %d should have expected value", i)
	}
}

func TestHeapQueuePriorityTiebreaker(t *testing.T) {
	assert := assert.New(t)
	// Specific test for the priority tiebreaker logic
	hq := &heapQueue[string]{
		items: make([]*enqItem[string], 0),
	}
	heap.Init(hq)

	// Add items with same priority but different indices
	heap.Push(hq, &enqItem[string]{Value: "first", Priority: 1, Index: 1})
	heap.Push(hq, &enqItem[string]{Value: "second", Priority: 1, Index: 0})
	
	// The item with lower index should be popped first
	first := heap.Pop(hq).(*enqItem[string])
	assert.Equal("second", first.Value, "Item with lower index should be popped first")
	
	second := heap.Pop(hq).(*enqItem[string])
	assert.Equal("first", second.Value, "Item with higher index should be popped second")
}
