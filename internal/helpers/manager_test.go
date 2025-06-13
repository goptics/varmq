package helpers

import (
	"fmt"
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
		manager := CreateManager[*testItem]()

		// Create test items
		item1 := &testItem{id: "item1", len: 3}
		item2 := &testItem{id: "item2", len: 5}
		item3 := &testItem{id: "item3", len: 2}

		// Test initial state
		assert.Equal(0, manager.Count())
		assert.Equal(0, manager.Len())

		// Register items
		manager.Register(item1)
		assert.Equal(1, manager.Count())
		assert.Equal(3, manager.Len())

		manager.Register(item2)
		manager.Register(item3)
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
			manager := CreateManager[*testItem]()
			item1 := &testItem{id: "item1", len: 1}
			item2 := &testItem{id: "item2", len: 2}
			item3 := &testItem{id: "item3", len: 3}

			// No items
			_, err := manager.GetRoundRobinItem()
			assert.Equal(ErrNoItemsRegistered, err)

			// Register & test order
			manager.Register(item1)
			res, err := manager.GetRoundRobinItem()
			assert.NoError(err)
			assert.Equal(item1, res)

			manager.Register(item2)
			manager.Register(item3)

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
			manager := CreateManager[*testItem]()
			a := &testItem{id: "a", len: 1}
			b := &testItem{id: "b", len: 1}
			c := &testItem{id: "c", len: 1}
			manager.Register(a)
			manager.Register(b)
			manager.Register(c)

			// Advance index to point to b (index 1)
			res, _ := manager.GetRoundRobinItem()
			assert.Equal(a, res)

			// Unregister the item currently pointed by roundRobinIndex (b)
			manager.UnregisterItem(b)
			assert.Equal(2, manager.Count())

			// After unregister, next call should not error and should return a according to current logic
			res, err := manager.GetRoundRobinItem()
			assert.NoError(err)
			assert.Equal(a, res)

			// Following call should return c (the remaining item)
			res, err = manager.GetRoundRobinItem()
			assert.NoError(err)
			assert.Equal(c, res)
		})

		t.Run("MaxLen", func(t *testing.T) {
			assert := assert.New(t)
			manager := CreateManager[*testItem]()
			item1 := &testItem{id: "item1", len: 3}
			item2 := &testItem{id: "item2", len: 5}
			item3 := &testItem{id: "item3", len: 2}
			item4 := &testItem{id: "item4", len: 7}

			_, err := manager.GetMaxLenItem()
			assert.Equal(ErrNoItemsRegistered, err)

			manager.Register(item1)
			manager.Register(item2)
			manager.Register(item3)
			manager.Register(item4)

			res, err := manager.GetMaxLenItem()
			assert.NoError(err)
			assert.Equal(item4, res)
		})

		t.Run("MinLen", func(t *testing.T) {
			assert := assert.New(t)
			manager := CreateManager[*testItem]()
			item1 := &testItem{id: "item1", len: 3}
			item2 := &testItem{id: "item2", len: 5}
			item3 := &testItem{id: "item3", len: 2}
			item4 := &testItem{id: "item4", len: 0}

			_, err := manager.GetMinLenItem()
			assert.Equal(ErrNoItemsRegistered, err)

			manager.Register(item1)
			manager.Register(item2)
			manager.Register(item3)
			res, _ := manager.GetMinLenItem()
			assert.Equal(item3, res)

			manager.Register(item4)
			res, _ = manager.GetMinLenItem()
			assert.Equal(item3, res)

			// only empty
			manager = CreateManager[*testItem]()
			empty := &testItem{id: "empty", len: 0}
			manager.Register(empty)
			res, _ = manager.GetMinLenItem()
			assert.Equal(empty, res)
		})
	})

	t.Run("EmptyQueue", func(t *testing.T) {
		assert := assert.New(t)
		manager := CreateManager[*testItem]()
		_, err := manager.GetRoundRobinItem()
		assert.Equal(ErrNoItemsRegistered, err)
		_, err = manager.GetMaxLenItem()
		assert.Equal(ErrNoItemsRegistered, err)
		_, err = manager.GetMinLenItem()
		assert.Equal(ErrNoItemsRegistered, err)
	})

	t.Run("ConcurrentOperations", func(t *testing.T) {
		assert := assert.New(t)
		manager := CreateManager[*testItem]()
		manager.Register(&testItem{id: "item1", len: 3})
		manager.Register(&testItem{id: "item2", len: 5})

		var wg sync.WaitGroup
		errChan := make(chan error, 30)
		for i := range 10 {
			wg.Add(3)
			go func(id int) {
				defer wg.Done()
				manager.Register(&testItem{id: fmt.Sprintf("item%d", id), len: id})
			}(i)
			go func() {
				defer wg.Done()
				if _, err := manager.GetRoundRobinItem(); err != nil && err != ErrNoItemsRegistered {
					errChan <- err
				}
			}()
			go func() {
				defer wg.Done()
				if _, err := manager.GetMaxLenItem(); err != nil && err != ErrNoItemsRegistered {
					errChan <- err
				}
			}()
		}
		wg.Wait()
		close(errChan)
		for err := range errChan {
			assert.NoError(err)
		}
		assert.Greater(manager.Count(), 2)
	})
}
