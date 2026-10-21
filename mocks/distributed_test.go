package mocks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockDistributedQueue(t *testing.T) {
	m := NewMockDistributedQueue()
	assert.NotNil(t, m)

	t.Run("Enqueue triggers subscribers", func(t *testing.T) {
		var action string
		m.Subscribe(func(act string) {
			action = act
		})

		ok := m.Enqueue("test")
		assert.True(t, ok)
		assert.Equal(t, "enqueued", action)
	})

	t.Run("Enqueue failure triggers no subscribers", func(t *testing.T) {
		var action string
		m.ShouldFailEnqueue = true
		m.Subscribe(func(act string) {
			action = act
		})

		ok := m.Enqueue("test")
		assert.False(t, ok)
		assert.Equal(t, "", action)
		m.ShouldFailEnqueue = false
	})
}

func TestMockDistributedPriorityQueue(t *testing.T) {
	m := NewMockDistributedPriorityQueue()
	assert.NotNil(t, m)

	t.Run("Enqueue triggers subscribers", func(t *testing.T) {
		var action string
		m.Subscribe(func(act string) {
			action = act
		})

		ok := m.Enqueue("test", 1)
		assert.True(t, ok)
		assert.Equal(t, "enqueued", action)
	})

	t.Run("Enqueue failure triggers no subscribers", func(t *testing.T) {
		var action string
		m.ShouldFailEnqueue = true
		m.Subscribe(func(act string) {
			action = act
		})

		ok := m.Enqueue("test", 1)
		assert.False(t, ok)
		assert.Equal(t, "", action)
		m.ShouldFailEnqueue = false
	})
}

// The distributed mocks' EnqueueOnce stores a repeat and notifies
// subscribers like Enqueue.
func TestMockDistributedEnqueueOnce(t *testing.T) {
	q := NewMockDistributedQueue()
	var n int
	q.Subscribe(func(string) { n++ })
	assert.True(t, q.EnqueueOnce("x", "a"))
	assert.True(t, q.EnqueueOnce("x", "a"))
	assert.Equal(t, 2, q.Len())
	assert.Equal(t, 2, n)
	q.ShouldFailEnqueue = true
	assert.False(t, q.EnqueueOnce("x", "b"))
	assert.Equal(t, 2, n, "a failed push is not announced")

	pq := NewMockDistributedPriorityQueue()
	var pn int
	pq.Subscribe(func(string) { pn++ })
	assert.True(t, pq.EnqueueOnce("x", 1, "a"))
	assert.True(t, pq.EnqueueOnce("x", 1, "a"))
	assert.Equal(t, 2, pq.Len())
	assert.Equal(t, 2, pn)
	pq.ShouldFailEnqueue = true
	assert.False(t, pq.EnqueueOnce("x", 1, "b"))
	assert.Equal(t, 2, pn)
}
