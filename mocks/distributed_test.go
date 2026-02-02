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
