package pool

import (
	"testing"

	"github.com/goptics/varmq/internal/linkedlist"
	"github.com/stretchr/testify/assert"
)

func TestNewPool(t *testing.T) {
	t.Run("creates pool with correct initialization", func(t *testing.T) {
		pool := NewPool[string](10)

		assert.NotNil(t, pool, "pool should not be nil")
		assert.NotNil(t, pool.List, "pool.List should not be nil")
		assert.Equal(t, 0, pool.Len(), "pool should start empty")

		// Test that the cache is properly initialized by getting a node
		node := pool.Cache.Get()
		assert.NotNil(t, node, "cache should return a valid node")

		// Verify the node type is correct
		_, ok := node.(*linkedlist.Node[Node[string]])
		assert.True(t, ok, "cache should return correct node type")

		// Put it back to avoid resource leaks
		pool.Cache.Put(node)
	})

	t.Run("verifies cache functionality", func(t *testing.T) {
		pool := NewPool[string](10)

		// Get a node, verify it's the correct type and functional
		node1 := pool.Cache.Get()
		typedNode1, ok := node1.(*linkedlist.Node[Node[string]])
		assert.True(t, ok, "cache should return correct node type")
		assert.NotNil(t, typedNode1.Value.ch, "node should have initialized channel")

		// Put it back and get another - both should be functional
		pool.Cache.Put(node1)
		node2 := pool.Cache.Get()
		typedNode2, ok := node2.(*linkedlist.Node[Node[string]])
		assert.True(t, ok, "cache should return correct node type")
		assert.NotNil(t, typedNode2.Value.ch, "node should have initialized channel")

		// Clean up
		pool.Cache.Put(node2)
	})

	t.Run("handles zero capacity", func(t *testing.T) {
		pool := NewPool[string](0)
		assert.NotNil(t, pool, "pool should handle zero capacity")

		node := pool.Cache.Get()
		assert.NotNil(t, node, "cache should work with zero capacity")
		pool.Cache.Put(node)
	})
}
