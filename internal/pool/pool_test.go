package pool

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPool(t *testing.T) {
	t.Run("creates pool with correct initialization", func(t *testing.T) {
		pool := New[string](10, 20)

		assert.NotNil(t, pool, "pool should not be nil")
		assert.Equal(t, 0, pool.Len(), "pool should start empty")
		assert.Equal(t, 10, pool.idleCap, "pool should have correct idle capacity")
		assert.Equal(t, 20, pool.expiredCap, "pool should have correct expired capacity")

		// Test that the cache is properly initialized by getting a node
		node := pool.Cache.Get()
		assert.NotNil(t, node, "cache should return a valid node")

		// Verify the node type is correct
		typedNode, ok := node.(*Node[string])
		assert.True(t, ok, "cache should return correct node type")
		assert.NotNil(t, typedNode.ch, "node should have initialized channel")

		// Put it back to avoid resource leaks
		pool.Cache.Put(node)
	})

	t.Run("verifies cache functionality", func(t *testing.T) {
		pool := New[string](10, 20)

		// Get a node, verify it's the correct type and functional
		node1 := pool.Cache.Get()
		typedNode1, ok := node1.(*Node[string])
		assert.True(t, ok, "cache should return correct node type")
		assert.NotNil(t, typedNode1.ch, "node should have initialized channel")

		// Put it back and get another - both should be functional
		pool.Cache.Put(node1)
		node2 := pool.Cache.Get()
		typedNode2, ok := node2.(*Node[string])
		assert.True(t, ok, "cache should return correct node type")
		assert.NotNil(t, typedNode2.ch, "node should have initialized channel")

		// Clean up
		pool.Cache.Put(node2)
	})

	t.Run("handles zero capacity", func(t *testing.T) {
		pool := New[string](0, 10)
		assert.NotNil(t, pool, "pool should handle zero capacity")
		assert.Equal(t, 0, pool.idleCap, "pool should have zero idle capacity")
		assert.Equal(t, 10, pool.expiredCap, "pool should have correct expired capacity")

		node := pool.Cache.Get()
		assert.NotNil(t, node, "cache should work with zero capacity")
		pool.Cache.Put(node)
	})

	t.Run("tests ChangeCapacities functionality", func(t *testing.T) {
		pool := New[string](5, 15)

		// Verify initial capacities
		assert.Equal(t, 5, pool.idleCap, "initial idle capacity should be 5")
		assert.Equal(t, 15, pool.expiredCap, "initial expired capacity should be 15")

		// Change capacities
		pool.ChangeCapacities(8, 25)

		// Verify updated capacities
		assert.Equal(t, 8, pool.idleCap, "idle capacity should be updated to 8")
		assert.Equal(t, 25, pool.expiredCap, "expired capacity should be updated to 25")
	})

	t.Run("tests push and pop operations with capacity limits", func(t *testing.T) {
		pool := New[string](2, 5)

		// Create test nodes
		node1 := NewNode[string](1)
		node2 := NewNode[string](1)
		node3 := NewNode[string](1)
		node4 := NewNode[string](1)

		// Test pushing to idle slice
		pool.PushNode(node1)
		assert.Equal(t, 1, pool.Len(), "pool should have 1 node")

		pool.PushNode(node2)
		assert.Equal(t, 2, pool.Len(), "pool should have 2 nodes")

		// Test pushing to expired slice (idle capacity exceeded)
		pool.PushNode(node3)
		assert.Equal(t, 3, pool.Len(), "pool should have 3 nodes")

		pool.PushNode(node4)
		assert.Equal(t, 4, pool.Len(), "pool should have 4 nodes")

		// Test popping (should pop from idle first)
		poppedNode := pool.PopBack()
		assert.Equal(t, node2, poppedNode, "should pop from idle slice first")
		assert.Equal(t, 3, pool.Len(), "pool should have 3 nodes after pop")

		// Pop again from idle
		poppedNode = pool.PopBack()
		assert.Equal(t, node1, poppedNode, "should pop remaining node from idle")
		assert.Equal(t, 2, pool.Len(), "pool should have 2 nodes after pop")

		// Pop from expired slice
		poppedNode = pool.PopBack()
		assert.Equal(t, node4, poppedNode, "should pop from expired slice")
		assert.Equal(t, 1, pool.Len(), "pool should have 1 node after pop")

		// Pop last node from expired slice
		poppedNode = pool.PopBack()
		assert.Equal(t, node3, poppedNode, "should pop last node from expired slice")
		assert.Equal(t, 0, pool.Len(), "pool should be empty after all pops")

		// Pop from empty pool
		poppedNode = pool.PopBack()
		assert.Nil(t, poppedNode, "should return nil when popping from empty pool")
	})

	t.Run("tests RemoveExpired functionality", func(t *testing.T) {
		pool := New[string](2, 5)

		// Create test nodes with different last used times
		node1 := NewNode[string](1)
		node2 := NewNode[string](1)
		node3 := NewNode[string](1)
		node4 := NewNode[string](1)

		// Set different last used times
		now := time.Now()
		node1.lastUsed.Store(now.Add(-2 * time.Hour))    // Old
		node2.lastUsed.Store(now.Add(-30 * time.Minute)) // Recent
		node3.lastUsed.Store(now.Add(-3 * time.Hour))    // Very old
		node4.lastUsed.Store(now.Add(-4 * time.Hour))    // Oldest

		pool.PushNode(node1)
		pool.PushNode(node2)
		pool.PushNode(node3) // This goes to expired slice
		pool.PushNode(node4) // This also goes to expired slice

		// Remove nodes older than 1 hour
		removedNodes := pool.RemoveExpired(1 * time.Hour)

		// Should remove expired nodes
		assert.Greater(t, len(removedNodes), 0, "should remove some expired nodes")
		assert.Less(t, pool.Len(), 4, "pool should have fewer nodes after removal")
	})

	t.Run("tests capacity boundary behavior", func(t *testing.T) {
		pool := New[string](3, 6)

		// Fill idle slice to capacity
		for i := 0; i < 3; i++ {
			node := NewNode[string](1)
			pool.PushNode(node)
		}
		assert.Equal(t, 3, pool.Len(), "pool should have 3 nodes in idle slice")

		// Add nodes to expired slice
		for i := 0; i < 3; i++ {
			node := NewNode[string](1)
			pool.PushNode(node)
		}
		assert.Equal(t, 6, pool.Len(), "pool should have 6 nodes total")

		// Verify popping order (idle first, then expired)
		for i := 0; i < 3; i++ {
			node := pool.PopBack()
			assert.NotNil(t, node, "should get valid node from idle slice")
		}
		assert.Equal(t, 3, pool.Len(), "should have 3 nodes left in expired slice")

		for i := 0; i < 3; i++ {
			node := pool.PopBack()
			assert.NotNil(t, node, "should get valid node from expired slice")
		}
		assert.Equal(t, 0, pool.Len(), "pool should be empty")
	})

	t.Run("tests RemoveExpired with capacity considerations", func(t *testing.T) {
		pool := New[string](2, 5)

		// Create nodes with times that make some expired
		now := time.Now()

		// Add nodes to idle slice (within capacity)
		node1 := NewNode[string](1)
		node1.lastUsed.Store(now.Add(-30 * time.Minute)) // Recent
		node2 := NewNode[string](1)
		node2.lastUsed.Store(now.Add(-2 * time.Hour)) // Old

		pool.PushNode(node1)
		pool.PushNode(node2)

		// Add nodes to expired slice (beyond idle capacity)
		node3 := NewNode[string](1)
		node3.lastUsed.Store(now.Add(-3 * time.Hour)) // Very old
		node4 := NewNode[string](1)
		node4.lastUsed.Store(now.Add(-4 * time.Hour)) // Oldest

		pool.PushNode(node3)
		pool.PushNode(node4)

		initialLen := pool.Len()

		// Remove nodes older than 1 hour
		removedNodes := pool.RemoveExpired(1 * time.Hour)

		// Should remove expired nodes from both slices
		assert.Greater(t, len(removedNodes), 0, "should remove some expired nodes")
		assert.Less(t, pool.Len(), initialLen, "pool should have fewer nodes after removal")

		// The recent node should still be in the pool
		assert.Greater(t, pool.Len(), 0, "pool should still have some nodes")
	})

	t.Run("tests RemoveExpired with idle capacity exceeded", func(t *testing.T) {
		pool := New[string](1, 5) // Very small idle capacity

		// Create nodes with different timestamps
		now := time.Now()

		// Add one node to idle slice
		node1 := NewNode[string](1)
		node1.lastUsed.Store(now.Add(-30 * time.Minute)) // Recent
		pool.PushNode(node1)

		// Add another node that will exceed idle capacity and go to expired
		node2 := NewNode[string](1)
		node2.lastUsed.Store(now.Add(-2 * time.Hour)) // Old - should be removed
		pool.PushNode(node2)                          // This should go to expired slice

		// Add more nodes to expired slice
		node3 := NewNode[string](1)
		node3.lastUsed.Store(now.Add(-3 * time.Hour)) // Very old - should be removed
		pool.PushNode(node3)

		assert.Equal(t, 3, pool.Len(), "pool should have 3 nodes total")

		// Remove nodes older than 1 hour
		removedNodes := pool.RemoveExpired(1 * time.Hour)

		// Should remove the old nodes from expired slice
		assert.Greater(t, len(removedNodes), 0, "should remove some expired nodes")
		assert.Equal(t, 1, pool.Len(), "pool should have 1 node remaining (the recent one)")

		// The remaining node should be the recent one
		remainingNode := pool.PopBack()
		assert.Equal(t, node1, remainingNode, "remaining node should be the recent one")
	})
}
