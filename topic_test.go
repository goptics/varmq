package varmq

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// waitOrTimeout fails the test if fn does not return within d.
func waitOrTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("timed out waiting")
	}
}

func TestTopic(t *testing.T) {
	t.Run("publish with zero subscribers", func(t *testing.T) {
		topic := NewTopic[int]()

		gj, ok := topic.Publish(1)

		assert.False(t, ok, "nothing was delivered")
		assert.NotNil(t, gj, "group job is still returned")
		assert.Equal(t, 0, gj.NumPending())
		assert.Equal(t, 0, topic.NumSubscribers())
		waitOrTimeout(t, time.Second, gj.Wait)
	})

	t.Run("close is idempotent and rejects publish", func(t *testing.T) {
		topic := NewTopic[int]()

		assert.NoError(t, topic.Close())
		assert.NoError(t, topic.Close())

		gj, ok := topic.Publish(1)

		assert.False(t, ok)
		assert.Nil(t, gj, "closed topic returns nil group job")
		assert.Equal(t, 0, topic.NumSubscribers())
	})
}
