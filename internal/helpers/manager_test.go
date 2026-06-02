package helpers

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// testItem implements the Sizer interface for testing
type testItem struct {
	id  string
	len int
}

func (i *testItem) Len() int {
	return i.len
}

// Grouped tests following project pattern
func TestManager(t *testing.T) {
	t.Run("BasicOperations", func(t *testing.T) {
		assert := assert.New(t)
		manager := NewManager[*testItem]()

		// Create test items
		item1 := &testItem{id: "item1", len: 3}
		item2 := &testItem{id: "item2", len: 5}
		item3 := &testItem{id: "item3", len: 2}

		// Test initial state
		assert.Equal(0, manager.Count())
		assert.Equal(0, manager.Len())

		// Register items
		manager.Register(item1, math.MaxInt)
		assert.Equal(1, manager.Count())
		assert.Equal(3, manager.Len())

		manager.Register(item2, math.MaxInt)
		manager.Register(item3, math.MaxInt)
		assert.Equal(3, manager.Count())
		assert.Equal(10, manager.Len())

		// Unregister item2
		manager.UnregisterItem(item2)
		assert.Equal(2, manager.Count())
		assert.Equal(5, manager.Len())
	})

	t.Run("Strategies", func(t *testing.T) {
		t.Run("RoundRobin", func(t *testing.T) {
			assert := assert.New(t)
			manager := NewManager[*testItem]()
			item1 := &testItem{id: "item1", len: 1}
			item2 := &testItem{id: "item2", len: 2}
			item3 := &testItem{id: "item3", len: 3}

			// No items
			_, ok := manager.GetRoundRobinItem()
			assert.False(ok)

			// Register & test order
			manager.Register(item1, math.MaxInt)
			res, ok := manager.GetRoundRobinItem()
			assert.True(ok)
			assert.Equal(item1, res)

			manager.Register(item2, math.MaxInt)
			manager.Register(item3, math.MaxInt)

			res, _ = manager.GetRoundRobinItem()
			assert.Equal(item1, res)
			res, _ = manager.GetRoundRobinItem()
			assert.Equal(item2, res)
			res, _ = manager.GetRoundRobinItem()
			assert.Equal(item3, res)
			res, _ = manager.GetRoundRobinItem()
			assert.Equal(item1, res)
		})

		t.Run("UnregisterUpdatesRoundRobinIndex", func(t *testing.T) {
			assert := assert.New(t)
			manager := NewManager[*testItem]()
			a := &testItem{id: "a", len: 1}
			b := &testItem{id: "b", len: 1}
			c := &testItem{id: "c", len: 1}
			manager.Register(a, math.MaxInt)
			manager.Register(b, math.MaxInt)
			manager.Register(c, math.MaxInt)

			// Advance index to point to b (index 1)
			res, _ := manager.GetRoundRobinItem()
			assert.Equal(a, res)

			// Unregister the item currently pointed by roundRobinIndex (b)
			manager.UnregisterItem(b)
			assert.Equal(2, manager.Count())

			// After unregister, next call should not error and should return a according to current logic
			res, ok := manager.GetRoundRobinItem()
			assert.True(ok)
			assert.Equal(a, res)

			// Following call should return c (the remaining item)
			res, ok = manager.GetRoundRobinItem()
			assert.True(ok)
			assert.Equal(c, res)
		})

		t.Run("MaxLen", func(t *testing.T) {
			assert := assert.New(t)
			manager := NewManager[*testItem]()
			item1 := &testItem{id: "item1", len: 3}
			item2 := &testItem{id: "item2", len: 5}
			item3 := &testItem{id: "item3", len: 2}
			item4 := &testItem{id: "item4", len: 7}

			_, ok := manager.GetMaxLenItem()
			assert.False(ok)

			manager.Register(item1, math.MaxInt)
			manager.Register(item2, math.MaxInt)
			manager.Register(item3, math.MaxInt)
			manager.Register(item4, math.MaxInt)

			res, ok := manager.GetMaxLenItem()
			assert.True(ok)
			assert.Equal(item4, res)
		})

		t.Run("MinLen", func(t *testing.T) {
			assert := assert.New(t)
			manager := NewManager[*testItem]()
			item1 := &testItem{id: "item1", len: 3}
			item2 := &testItem{id: "item2", len: 5}
			item3 := &testItem{id: "item3", len: 2}
			item4 := &testItem{id: "item4", len: 0}

			_, ok := manager.GetMinLenItem()
			assert.False(ok)

			manager.Register(item1, math.MaxInt)
			manager.Register(item2, math.MaxInt)
			manager.Register(item3, math.MaxInt)
			res, _ := manager.GetMinLenItem()
			assert.Equal(item3, res)

			manager.Register(item4, math.MaxInt)
			res, _ = manager.GetMinLenItem()
			assert.Equal(item3, res)
		})

		t.Run("Priority", func(t *testing.T) {
			assert := assert.New(t)
			manager := NewManager[*testItem]()
			item1 := &testItem{id: "item1", len: 1} // lowest priority (high number)
			item2 := &testItem{id: "item2", len: 2} // mid priority
			item3 := &testItem{id: "item3", len: 0} // highest priority but empty
			item4 := &testItem{id: "item4", len: 4} // high priority

			_, ok := manager.GetPriorityItem()
			assert.False(ok)

			manager.Register(item1, 10) // priority 10
			manager.Register(item2, 5)  // priority 5

			res, ok := manager.GetPriorityItem()
			assert.True(ok)
			assert.Equal(item2, res) // Returns highest priority item with Len > 0 (item2 with priority 5)

			manager.Register(item3, 1) // priority 1

			res, ok = manager.GetPriorityItem()
			assert.True(ok)
			assert.Equal(item2, res) // item3 is empty, so should still return item2

			manager.Register(item4, 3) // priority 3

			res, ok = manager.GetPriorityItem()
			assert.True(ok)
			assert.Equal(item4, res) // Returns item4 as it has higher priority (3) than item2 (5)
		})

		// All methods should return false when only zero-length items are registered
		t.Run("AllItemsEmpty", func(t *testing.T) {
			assert := assert.New(t)
			manager := NewManager[*testItem]()
			manager.Register(&testItem{id: "e1", len: 0}, math.MaxInt)
			manager.Register(&testItem{id: "e2", len: 0}, math.MaxInt)

			_, ok := manager.GetRoundRobinItem()
			assert.False(ok)

			_, ok = manager.GetMaxLenItem()
			assert.False(ok)

			_, ok = manager.GetMinLenItem()
			assert.False(ok)

			_, ok = manager.GetPriorityItem()
			assert.False(ok)
		})
	})

	t.Run("EmptyManager", func(t *testing.T) {
		assert := assert.New(t)
		manager := NewManager[*testItem]()
		_, ok := manager.GetRoundRobinItem()
		assert.False(ok)
		_, ok = manager.GetMaxLenItem()
		assert.False(ok)
		_, ok = manager.GetMinLenItem()
		assert.False(ok)
		_, ok = manager.GetPriorityItem()
		assert.False(ok)
	})

	t.Run("ConcurrentOperations", func(t *testing.T) {
		assert := assert.New(t)
		manager := NewManager[*testItem]()
		manager.Register(&testItem{id: "item1", len: 3}, math.MaxInt)
		manager.Register(&testItem{id: "item2", len: 5}, math.MaxInt)

		var wg sync.WaitGroup
		for i := range 10 {
			wg.Add(3)
			go func(id int) {
				defer wg.Done()
				manager.Register(&testItem{id: fmt.Sprintf("item%d", id), len: id}, math.MaxInt)
			}(i)
			go func() {
				defer wg.Done()
				manager.GetRoundRobinItem()
			}()
			go func() {
				defer wg.Done()
				manager.GetMaxLenItem()
			}()
		}
		wg.Wait()
		assert.Greater(manager.Count(), 2)
	})

	t.Run("PriorityRegistration", func(t *testing.T) {
		assert := assert.New(t)
		manager := NewManager[*testItem]()

		itemLow := &testItem{id: "low", len: 1}
		itemMedium := &testItem{id: "medium", len: 2}
		itemHigh := &testItem{id: "high", len: 3}
		itemHigh2 := &testItem{id: "high2", len: 4}

		manager.Register(itemLow, 10)
		manager.Register(itemHigh, 1)
		manager.Register(itemMedium, 5)

		// Same priority should preserve insertion order
		manager.Register(itemHigh2, 1)

		// Order should be: high, high2, medium, low
		assert.Equal(4, manager.Count())

		res, ok := manager.GetRoundRobinItem()
		assert.True(ok)
		assert.Equal(itemHigh, res)

		res, ok = manager.GetRoundRobinItem()
		assert.True(ok)
		assert.Equal(itemHigh2, res)

		res, ok = manager.GetRoundRobinItem()
		assert.True(ok)
		assert.Equal(itemMedium, res)

		res, ok = manager.GetRoundRobinItem()
		assert.True(ok)
		assert.Equal(itemLow, res)

		// Unregistering item should maintain order of the rest
		manager.UnregisterItem(itemMedium)

		// Next item was index 0 (high)
		res, ok = manager.GetRoundRobinItem()
		assert.True(ok)
		assert.Equal(itemHigh, res)
	})
}
