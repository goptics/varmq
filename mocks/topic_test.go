package mocks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockPersistentTopic(t *testing.T) {
	t.Run("groups fan out and readers of one group share it", func(t *testing.T) {
		m := NewMockPersistentTopic()
		assert.NoError(t, m.JoinGroup("a"))
		assert.NoError(t, m.JoinGroup("b"))

		assert.True(t, m.Publish("x"))
		assert.Equal(t, 1, m.GroupLen("a"))
		assert.Equal(t, 1, m.GroupLen("b"))

		item, ok, ack := m.DequeueGroup("a")
		assert.True(t, ok)
		assert.Equal(t, "x", item)
		assert.Equal(t, "1", ack)
		assert.Equal(t, 0, m.GroupLen("a"))
		assert.Equal(t, 1, m.GroupLen("b"))
	})

	t.Run("join is idempotent and starts at the head", func(t *testing.T) {
		m := NewMockPersistentTopic()
		assert.NoError(t, m.JoinGroup("a"))
		m.Publish("x")
		assert.NoError(t, m.JoinGroup("a"))
		assert.Equal(t, 1, m.GroupLen("a"))

		assert.NoError(t, m.JoinGroup("late"))
		assert.Equal(t, 0, m.GroupLen("late"))
	})

	t.Run("acknowledge, purge, delete", func(t *testing.T) {
		m := NewMockPersistentTopic()
		assert.NoError(t, m.JoinGroup("a"))
		m.Publish("x")
		m.Publish("y")

		_, ok, ack := m.DequeueGroup("a")
		assert.True(t, ok)
		assert.Equal(t, 1, m.PendingCount("a"))

		assert.True(t, m.AcknowledgeGroup("a", ack))
		assert.Equal(t, 0, m.PendingCount("a"))
		assert.False(t, m.AcknowledgeGroup("a", ack))
		assert.False(t, m.AcknowledgeGroup("missing", "1"))
		assert.Equal(t, []string{"a:1"}, m.AckedIDs())

		m.PurgeGroup("a")
		assert.Equal(t, 0, m.GroupLen("a"))

		assert.True(t, m.HasGroup("a"))
		assert.NoError(t, m.DeleteGroup("a"))
		assert.False(t, m.HasGroup("a"))
		_, ok, _ = m.DequeueGroup("a")
		assert.False(t, ok)
	})

	t.Run("acknowledge after close fails", func(t *testing.T) {
		m := NewMockPersistentTopic()
		assert.NoError(t, m.JoinGroup("a"))
		m.Publish("x")
		_, ok, ack := m.DequeueGroup("a")
		assert.True(t, ok)

		assert.NoError(t, m.Close())
		assert.False(t, m.AcknowledgeGroup("a", ack))
	})

	t.Run("delete then rejoin starts at the head", func(t *testing.T) {
		m := NewMockPersistentTopic()
		assert.NoError(t, m.JoinGroup("a"))
		m.Publish("x")
		assert.NoError(t, m.DeleteGroup("a"))
		assert.NoError(t, m.JoinGroup("a"))
		assert.Equal(t, 0, m.GroupLen("a"))

		m.Publish("y")
		item, ok, _ := m.DequeueGroup("a")
		assert.True(t, ok)
		assert.Equal(t, "y", item)
	})

	t.Run("failure switches", func(t *testing.T) {
		m := NewMockPersistentTopic()
		m.ShouldFailJoin = true
		assert.Error(t, m.JoinGroup("a"))

		m.ShouldFailPublish = true
		assert.False(t, m.Publish("x"))

		m.ShouldFailAcknowledge = true
		assert.False(t, m.AcknowledgeGroup("a", "1"))
	})

	t.Run("close rejects publish", func(t *testing.T) {
		m := NewMockPersistentTopic()
		assert.NoError(t, m.Close())
		assert.False(t, m.Publish("x"))
	})

	t.Run("unknown group reads as empty", func(t *testing.T) {
		m := NewMockPersistentTopic()

		assert.Equal(t, 0, m.GroupLen("missing"))
		assert.Equal(t, 0, m.PendingCount("missing"))
		_, ok, _ := m.DequeueGroup("missing")
		assert.False(t, ok)
		m.PurgeGroup("missing")
		assert.NoError(t, m.DeleteGroup("missing"))
	})
}
