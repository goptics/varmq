package mocks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockPersistentQueue(t *testing.T) {
	m := NewMockPersistentQueue()
	assert.NotNil(t, m)

	t.Run("Enqueue and Dequeue", func(t *testing.T) {
		ok := m.Enqueue("test")
		assert.True(t, ok)
		assert.Equal(t, 1, m.Len())

		val, ok := m.Dequeue()
		assert.True(t, ok)
		assert.Equal(t, "test", val)
	})

	t.Run("Enqueue failure flag", func(t *testing.T) {
		m.ShouldFailEnqueue = true
		ok := m.Enqueue("fail")
		assert.False(t, ok)
		m.ShouldFailEnqueue = false
	})

	t.Run("Acknowledge", func(t *testing.T) {
		ok := m.Acknowledge("id")
		assert.True(t, ok)

		m.ShouldFailAcknowledge = true
		ok = m.Acknowledge("id")
		assert.False(t, ok)
		m.ShouldFailAcknowledge = false
	})

	t.Run("DequeueWithAckId", func(t *testing.T) {
		m.Enqueue("test-ack")
		val, ok, ackID := m.DequeueWithAckId()
		assert.True(t, ok)
		assert.Equal(t, "test-ack", val)
		assert.Equal(t, "mock-ack-id", ackID)
	})
}

func TestMockPersistentPriorityQueue(t *testing.T) {
	m := NewMockPersistentPriorityQueue()
	assert.NotNil(t, m)

	t.Run("Enqueue and Dequeue", func(t *testing.T) {
		ok := m.Enqueue("test", 1)
		assert.True(t, ok)
		assert.Equal(t, 1, m.Len())

		val, ok := m.Dequeue()
		assert.True(t, ok)
		assert.Equal(t, "test", val)
	})

	t.Run("Enqueue failure flag", func(t *testing.T) {
		m.ShouldFailEnqueue = true
		ok := m.Enqueue("fail", 1)
		assert.False(t, ok)
		m.ShouldFailEnqueue = false
	})

	t.Run("Acknowledge", func(t *testing.T) {
		ok := m.Acknowledge("id")
		assert.True(t, ok)

		m.ShouldFailAcknowledge = true
		ok = m.Acknowledge("id")
		assert.False(t, ok)
		m.ShouldFailAcknowledge = false
	})

	t.Run("DequeueWithAckId", func(t *testing.T) {
		m.Enqueue("test-ack-p", 1)
		val, ok, ackID := m.DequeueWithAckId()
		assert.True(t, ok)
		assert.Equal(t, "test-ack-p", val)
		assert.Equal(t, "mock-ack-id", ackID)
	})
}

// The mocks shadow the embedded in-memory EnqueueOnce: a repeated id is
// stored again, and ShouldFailEnqueue still applies.
func TestMockPersistentEnqueueOnce(t *testing.T) {
	q := NewMockPersistentQueue()
	assert.True(t, q.EnqueueOnce("x", "a"))
	assert.True(t, q.EnqueueOnce("x", "a"))
	assert.Equal(t, 2, q.Len())
	q.ShouldFailEnqueue = true
	assert.False(t, q.EnqueueOnce("x", "b"))

	pq := NewMockPersistentPriorityQueue()
	assert.True(t, pq.EnqueueOnce("x", 1, "a"))
	assert.True(t, pq.EnqueueOnce("x", 1, "a"))
	assert.Equal(t, 2, pq.Len())
	pq.ShouldFailEnqueue = true
	assert.False(t, pq.EnqueueOnce("x", 1, "b"))
}
