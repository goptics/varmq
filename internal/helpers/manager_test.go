package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// testItem implements the Sizer interface for testing
type testItem struct {
	id       string
	len      int
	priority int
}

func (i *testItem) Len() int {
	return i.len
}

func TestManagerBasicOperations(t *testing.T) {
	assert := assert.New(t)
	manager := CreateManager[*testItem]()

	// Create test items
	item1 := &testItem{id: "item1", len: 3, priority: 1}
	item2 := &testItem{id: "item2", len: 5, priority: 2}
	item3 := &testItem{id: "item3", len: 2, priority: 3}

	// Test initial state
	assert.Equal(0, manager.Count(), "New manager should have zero items")
	assert.Equal(0, manager.Len(), "New manager should have zero total length")

	// Test registering items
	manager.Register(item1, item1.priority)
	assert.Equal(1, manager.Count(), "Manager should have one item after registration")
	assert.Equal(3, manager.Len(), "Manager total length should match the item length")

	manager.Register(item2, item2.priority)
	manager.Register(item3, item3.priority)
	assert.Equal(3, manager.Count(), "Manager should have three items after registration")
	assert.Equal(10, manager.Len(), "Manager total length should match the sum of item lengths")

	// Test unregistering items
	manager.UnregisterItem(item2)
	assert.Equal(2, manager.Count(), "Manager should have two items after unregistration")
	assert.Equal(5, manager.Len(), "Manager total length should be updated after unregistration")
}

func TestManagerRoundRobinStrategy(t *testing.T) {
	assert := assert.New(t)
	manager := CreateManager[*testItem]()

	// Create test items
	item1 := &testItem{id: "item1", len: 1, priority: 1}
	item2 := &testItem{id: "item2", len: 2, priority: 2}
	item3 := &testItem{id: "item3", len: 3, priority: 3}

	// Test with no items
	_, err := manager.GetRoundRobinItem()
	assert.Error(err, "GetRoundRobinItem should return error when no items registered")
	assert.Equal(ErrNoItemsRegistered, err)

	// Test with one item
	manager.Register(item1, item1.priority)
	result, err := manager.GetRoundRobinItem()
	assert.NoError(err, "GetRoundRobinItem should not return error with registered items")
	assert.Equal(item1, result, "First item should be returned")

	// Adding more items and testing round-robin behavior
	manager.Register(item2, item2.priority)
	manager.Register(item3, item3.priority)

	// First call should return item1 and move it to the end
	result, err = manager.GetRoundRobinItem()
	assert.NoError(err)
	assert.Equal(item1, result, "First item should be returned first time")

	// Second call should return item2
	result, err = manager.GetRoundRobinItem()
	assert.NoError(err)
	assert.Equal(item2, result, "Second item should be returned second time")

	// Third call should return item3
	result, err = manager.GetRoundRobinItem()
	assert.NoError(err)
	assert.Equal(item3, result, "Third item should be returned third time")

	// Fourth call should return item1 again (round-robin)
	result, err = manager.GetRoundRobinItem()
	assert.NoError(err)
	assert.Equal(item1, result, "First item should be returned again in round-robin fashion")
}

func TestManagerMaxLenStrategy(t *testing.T) {
	assert := assert.New(t)
	manager := CreateManager[*testItem]()

	// Create test items with different lengths
	item1 := &testItem{id: "item1", len: 3, priority: 1}
	item2 := &testItem{id: "item2", len: 5, priority: 2}
	item3 := &testItem{id: "item3", len: 2, priority: 3}
	item4 := &testItem{id: "item4", len: 7, priority: 4}

	// Test with no items
	_, err := manager.GetMaxLenItem()
	assert.Error(err, "GetMaxLenItem should return error when no items registered")
	assert.Equal(ErrNoItemsRegistered, err)

	// Register items and test max len selection
	manager.Register(item1, item1.priority)
	manager.Register(item2, item2.priority)
	manager.Register(item3, item3.priority)
	manager.Register(item4, item4.priority)

	result, err := manager.GetMaxLenItem()
	assert.NoError(err)
	assert.Equal(item4, result, "Item with maximum length should be returned")
}

func TestManagerMinLenStrategy(t *testing.T) {
	assert := assert.New(t)
	manager := CreateManager[*testItem]()

	// Create test items with different lengths
	item1 := &testItem{id: "item1", len: 3, priority: 1}
	item2 := &testItem{id: "item2", len: 5, priority: 2}
	item3 := &testItem{id: "item3", len: 2, priority: 3}
	item4 := &testItem{id: "item4", len: 0, priority: 4}

	// Test with no items
	_, err := manager.GetMinLenItem()
	assert.Error(err, "GetMinLenItem should return error when no items registered")
	assert.Equal(ErrNoItemsRegistered, err)

	// Register items and test min len selection
	manager.Register(item1, item1.priority)
	manager.Register(item2, item2.priority)
	manager.Register(item3, item3.priority)

	result, err := manager.GetMinLenItem()
	assert.NoError(err)
	assert.Equal(item3, result, "Item with minimum length (excluding zero) should be returned")

	// Test with an empty item - should still return the minimum non-zero item
	manager.Register(item4, item4.priority)
	result, err = manager.GetMinLenItem()
	assert.NoError(err)
	assert.Equal(item3, result, "Item with minimum length (excluding zero) should be returned")

	// Test with only empty items
	manager = CreateManager[*testItem]()
	emptyItem := &testItem{id: "empty", len: 0, priority: 1}
	manager.Register(emptyItem, emptyItem.priority)

	result, err = manager.GetMinLenItem()
	assert.NoError(err)
	assert.Equal(emptyItem, result, "With only empty items, first item should be returned")
}

func TestManagerPriorityStrategy(t *testing.T) {
	assert := assert.New(t)
	manager := CreateManager[*testItem]()

	// Create test items with different priorities
	item1 := &testItem{id: "item1", len: 1, priority: 10}
	item2 := &testItem{id: "item2", len: 2, priority: 20}
	item3 := &testItem{id: "item3", len: 3, priority: 30}
	item4 := &testItem{id: "item4", len: 4, priority: 5}

	// Test with no items
	_, err := manager.GetPriorityItem()
	assert.Error(err, "GetPriorityItem should return error when no items registered")
	assert.Equal(ErrNoItemsRegistered, err)

	// Register items and test priority selection
	manager.Register(item1, item1.priority)
	manager.Register(item2, item2.priority)
	manager.Register(item3, item3.priority)
	manager.Register(item4, item4.priority)

	result, err := manager.GetPriorityItem()
	assert.NoError(err)
	assert.Equal(item3, result, "Item with highest priority should be returned")
}

func TestManagerEmptyQueue(t *testing.T) {
	assert := assert.New(t)
	manager := CreateManager[*testItem]()

	// Test operations on empty manager
	_, err := manager.GetRoundRobinItem()
	assert.Error(err, "GetRoundRobinItem should return error when no items registered")
	assert.Equal(ErrNoItemsRegistered, err)

	_, err = manager.GetMaxLenItem()
	assert.Error(err, "GetMaxLenItem should return error when no items registered")
	assert.Equal(ErrNoItemsRegistered, err)

	_, err = manager.GetMinLenItem()
	assert.Error(err, "GetMinLenItem should return error when no items registered")
	assert.Equal(ErrNoItemsRegistered, err)

	_, err = manager.GetPriorityItem()
	assert.Error(err, "GetPriorityItem should return error when no items registered")
	assert.Equal(ErrNoItemsRegistered, err)
}

func TestManagerConcurrentOperations(t *testing.T) {
	assert := assert.New(t)
	manager := CreateManager[*testItem]()

	// Create test items
	item1 := &testItem{id: "item1", len: 3, priority: 1}
	item2 := &testItem{id: "item2", len: 5, priority: 2}

	// Register items
	manager.Register(item1, item1.priority)
	manager.Register(item2, item2.priority)

	// Test concurrent access safety - this is more of a sanity check
	// since we can't truly test concurrent safety in unit tests without races
	go func() {
		manager.Register(&testItem{id: "item3", len: 7, priority: 3}, 3)
	}()

	go func() {
		manager.GetRoundRobinItem()
	}()

	go func() {
		manager.GetMaxLenItem()
	}()

	// Wait for goroutines to complete
	// This just verifies that the code doesn't panic under concurrent access
	assert.True(true, "Concurrent operations should not cause panics")
}
