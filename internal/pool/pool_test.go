package pool

import (
	"testing"

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

		// Put it back to avoid resource leaks
		pool.Cache.Put(node)
	})
}
