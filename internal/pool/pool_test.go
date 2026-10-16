package pool

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPool(t *testing.T) {
	t.Run("creates pool with correct initialization", func(t *testing.T) {
		pool := New[string](10)

		assert.NotNil(t, pool, "pool should not be nil")
		assert.Equal(t, 0, pool.Len(), "pool should start empty")

		// Test that the cache is properly initialized by getting a node
		node := pool.Cache.Get()
		assert.NotNil(t, node, "cache should return a valid node")

		// Verify the node type is correct
		_, ok := node.(*Elem[string])
		assert.True(t, ok, "cache should return correct node type")

		// Put it back to avoid resource leaks
		pool.Cache.Put(node)
	})

	t.Run("verifies cache functionality", func(t *testing.T) {
		pool := New[string](10)

		// Get a node, verify it's the correct type and functional
		node1 := pool.Cache.Get()
		typedNode1, ok := node1.(*Elem[string])
		assert.True(t, ok, "cache should return correct node type")
		assert.NotNil(t, typedNode1.Value.ch, "node should have initialized channel")

		// Put it back and get another - both should be functional
		pool.Cache.Put(node1)
		node2 := pool.Cache.Get()
		typedNode2, ok := node2.(*Elem[string])
		assert.True(t, ok, "cache should return correct node type")
		assert.NotNil(t, typedNode2.Value.ch, "node should have initialized channel")

		// Clean up
		pool.Cache.Put(node2)
	})

	t.Run("handles zero capacity", func(t *testing.T) {
		pool := New[string](0)
		assert.NotNil(t, pool, "pool should handle zero capacity")

		node := pool.Cache.Get()
		assert.NotNil(t, node, "cache should work with zero capacity")
		pool.Cache.Put(node)
	})

	t.Run("stack is LIFO and prune keeps the bottom nodes", func(t *testing.T) {
		pool := New[int](1)
		elems := make([]*Elem[int], 5)
		for i := range elems {
			elems[i] = pool.Cache.Get().(*Elem[int])
			pool.PushNode(elems[i])
		}

		// Drop everything prunable: the bottom 2 stay.
		dropped := pool.Prune(2, func(*Elem[int]) bool { return true })
		assert.Equal(t, []*Elem[int]{elems[4], elems[3], elems[2]}, dropped)
		assert.Equal(t, 2, pool.Len())

		// Drop from the middle only.
		pool.PushNode(elems[2])
		pool.PushNode(elems[3])
		dropped = pool.Prune(0, func(e *Elem[int]) bool { return e == elems[2] || e == elems[0] })
		assert.Equal(t, []*Elem[int]{elems[2], elems[0]}, dropped)

		assert.Same(t, elems[3], pool.PopBack())
		assert.Same(t, elems[1], pool.PopBack())
		assert.Nil(t, pool.PopBack())
		assert.Equal(t, 0, pool.Len())
	})

	t.Run("concurrent push, pop and prune lose no node", func(t *testing.T) {
		pool := New[int](1)
		const pushers, rounds = 8, 2000

		var wg sync.WaitGroup
		for range pushers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				e := pool.Cache.Get().(*Elem[int])
				for range rounds {
					pool.PushNode(e)
					if e = pool.PopBack(); e == nil {
						// Pruned or taken by another goroutine: use a fresh one.
						e = pool.Cache.Get().(*Elem[int])
					}
				}
				pool.PushNode(e)
			}()
		}
		done := make(chan struct{})
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					pool.Prune(1, func(*Elem[int]) bool { return true })
				}
			}
		}()
		wg.Wait()
		close(done)

		// The count matches the nodes still linked.
		want, count := pool.Len(), 0
		for pool.PopBack() != nil {
			count++
		}
		assert.Equal(t, want, count)
		assert.Positive(t, count)
	})
}
