package linkedlist

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinkedList(t *testing.T) {
	t.Run("Initialization", func(t *testing.T) {
		assert := assert.New(t)
		list := New[int]()

		assert.Equal(0, list.Len(), "new list should have length 0")
		assert.Nil(list.Front(), "new list should have nil front")
		assert.Nil(list.Back(), "new list should have nil back")
	})

	t.Run("PushFront", func(t *testing.T) {
		assert := assert.New(t)
		list := New[int]()

		// Add one element
		node1 := list.PushFront(1)
		assert.Equal(1, list.Len(), "list should have length 1 after pushing one element")
		assert.Equal(node1, list.Front(), "front should be the newly pushed node")
		assert.Equal(node1, list.Back(), "back should be the newly pushed node for a list with one element")

		// Add another element
		node2 := list.PushFront(2)
		assert.Equal(2, list.Len(), "list should have length 2 after pushing another element")
		assert.Equal(node2, list.Front(), "front should be the newly pushed node")
		assert.Equal(node1, list.Back(), "back should still be the first node")
	})

	t.Run("PushBack", func(t *testing.T) {
		assert := assert.New(t)
		list := New[int]()

		// Add one element
		node1 := list.PushBack(1)
		assert.Equal(1, list.Len(), "list should have length 1 after pushing one element")
		assert.Equal(node1, list.Front(), "front should be the newly pushed node")
		assert.Equal(node1, list.Back(), "back should be the newly pushed node for a list with one element")

		// Add another element
		node2 := list.PushBack(2)
		assert.Equal(2, list.Len(), "list should have length 2 after pushing another element")
		assert.Equal(node1, list.Front(), "front should still be the first node")
		assert.Equal(node2, list.Back(), "back should be the newly pushed node")
	})

	t.Run("InsertAfter", func(t *testing.T) {
		assert := assert.New(t)
		list := New[int]()

		// Add two elements
		node1 := list.PushBack(1)
		node3 := list.PushBack(3)

		// Insert between them
		node2 := list.InsertAfter(2, node1)
		assert.Equal(3, list.Len(), "list should have length 3 after insertion")

		// Check the order
		assert.Equal(node1, list.Front(), "front should be the first node")
		assert.Equal(node2, node1.Next(), "node1.Next should point to node2")
		assert.Equal(node3, node2.Next(), "node2.Next should point to node3")
		assert.Equal(node3, list.Back(), "back should be the last node")
	})

	t.Run("Remove", func(t *testing.T) {
		assert := assert.New(t)
		list := New[int]()

		// Create a list with three nodes
		node1 := list.PushBack(1)
		node2 := list.PushBack(2)
		node3 := list.PushBack(3)

		// Test removing the middle node
		success := list.Remove(node2)
		assert.True(success, "Remove should return true for valid node")
		assert.Equal(2, list.Len(), "list should have length 2 after removal")
		assert.Equal(node3, node1.Next(), "node1.Next should be node3 after removing node2")
		assert.Equal(node1, node3.Prev(), "node3.Prev should be node1 after removing node2")

		// Test removing the first node
		success = list.Remove(node1)
		assert.True(success, "Remove should return true for first node")
		assert.Equal(1, list.Len(), "list should have length 1 after removal")
		assert.Equal(node3, list.Front(), "Front should be node3 after removing node1")
		assert.Equal(node3, list.Back(), "Back should be node3 after removing node1")

		// Test removing the last node
		success = list.Remove(node3)
		assert.True(success, "Remove should return true for last node")
		assert.Equal(0, list.Len(), "list should have length 0 after removal")
		assert.Nil(list.Front(), "Front should be nil after removing last node")
		assert.Nil(list.Back(), "Back should be nil after removing last node")

		// Test removing an already removed node
		success = list.Remove(node3)
		assert.False(success, "Remove should return false for already removed node")
	})

	t.Run("PopFront", func(t *testing.T) {
		assert := assert.New(t)
		list := New[int]()

		// Test pop from empty list
		node := list.PopFront()
		assert.Nil(node, "PopFront should return nil for empty list")

		// Add two elements
		list.PushBack(1)
		list.PushBack(2)

		// Test popping first element
		node = list.PopFront()
		assert.NotNil(node, "PopFront should not return nil for non-empty list")
		assert.Equal(1, node.Value, "PopFront should return the first element")
		assert.Equal(1, list.Len(), "list should have length 1 after popping")

		// Test popping second element
		node = list.PopFront()
		assert.NotNil(node, "PopFront should not return nil for non-empty list")
		assert.Equal(2, node.Value, "PopFront should return the first element")
		assert.Equal(0, list.Len(), "list should have length 0 after popping")
	})

	t.Run("PopBack", func(t *testing.T) {
		assert := assert.New(t)
		list := New[int]()

		// Test pop from empty list
		node := list.PopBack()
		assert.Nil(node, "PopBack should return nil for empty list")

		// Add two elements
		list.PushBack(1)
		list.PushBack(2)

		// Test popping last element
		node = list.PopBack()
		assert.NotNil(node, "PopBack should not return nil for non-empty list")
		assert.Equal(2, node.Value, "PopBack should return the last element")
		assert.Equal(1, list.Len(), "list should have length 1 after popping")

		// Test popping remaining element
		node = list.PopBack()
		assert.NotNil(node, "PopBack should not return nil for non-empty list")
		assert.Equal(1, node.Value, "PopBack should return the last element")
		assert.Equal(0, list.Len(), "list should have length 0 after popping")
	})

	t.Run("NodeSlice", func(t *testing.T) {
		assert := assert.New(t)
		list := New[int]()

		// Test empty list
		slice := list.NodeSlice()
		assert.Equal(0, len(slice), "slice from empty list should be empty")

		// Add three elements
		node1 := list.PushBack(1)
		node2 := list.PushBack(2)
		node3 := list.PushBack(3)

		// Check slice contents
		slice = list.NodeSlice()
		assert.Equal(3, len(slice), "slice should have length 3")
		assert.Equal([]*Node[int]{node1, node2, node3}, slice, "slice should contain all nodes in order")
	})

	t.Run("ConcurrentOperations", func(t *testing.T) {
		assert := assert.New(t)
		list := New[int]()
		wg := sync.WaitGroup{}

		// Add 100 items concurrently
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(val int) {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					list.PushBack(val*10 + j)
				}
			}(i)
		}
		wg.Wait()

		assert.Equal(100, list.Len(), "list should have 100 items after concurrent additions")

		// Remove half the items concurrently
		nodes := list.NodeSlice()
		wg = sync.WaitGroup{}
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(node *Node[int]) {
				defer wg.Done()
				list.Remove(node)
			}(nodes[i])
		}
		wg.Wait()

		assert.Equal(50, list.Len(), "list should have 50 items after concurrent removals")
	})

	t.Run("PushNode", func(t *testing.T) {
		assert := assert.New(t)
		list1 := New[int]()
		list2 := New[int]()

		// Add a node to list1
		node := list1.PushBack(42)

		// Remove it from list1
		list1.Remove(node)

		// Push it to list2
		list2.PushNode(node)

		assert.Equal(0, list1.Len(), "list1 should have length 0")
		assert.Equal(1, list2.Len(), "list2 should have length 1")
		assert.Equal(node, list2.Front(), "list2 front should be the pushed node")
	})

	t.Run("RemoveSentinel", func(t *testing.T) {
		assert := assert.New(t)
		list := New[int]()

		// Try to remove the sentinel node
		// This is done by accessing the private root field, which is not recommended
		// in practice but useful for testing this edge case
		success := list.Remove(&list.root)

		assert.False(success, "Remove should return false for sentinel node")
		assert.Equal(0, list.Len(), "list length should remain 0")
	})
}
