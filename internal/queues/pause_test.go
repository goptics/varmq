package queues

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A paused queue hands out nothing; adds still land; hooks see each change once.
func TestPause(t *testing.T) {
	for name, q := range map[string]interface {
		Pause() error
		Resume() error
		Paused() bool
		PausedAt() int64
		OnPauseChange(func(bool)) func()
		Dequeue() (any, bool)
		Len() int
	}{"fifo": NewQueue[int](), "priority": NewPriorityQueue[int]()} {
		t.Run(name, func(t *testing.T) {
			var seen []bool
			cancel := q.OnPauseChange(func(p bool) { seen = append(seen, p) })

			require.NoError(t, q.Pause())
			at := q.PausedAt()
			assert.NotZero(t, at)
			require.NoError(t, q.Pause())
			assert.Equal(t, at, q.PausedAt(), "pausing again keeps the first time")

			switch s := q.(type) {
			case *Queue[int]:
				assert.True(t, s.Enqueue(1))
			case *PriorityQueue[int]:
				assert.True(t, s.Enqueue(1, 0))
			}
			assert.Equal(t, 1, q.Len())
			_, ok := q.Dequeue()
			assert.False(t, ok, "paused")

			require.NoError(t, q.Resume())
			require.NoError(t, q.Resume())
			assert.False(t, q.Paused())
			assert.Zero(t, q.PausedAt())
			v, ok := q.Dequeue()
			assert.True(t, ok)
			assert.Equal(t, 1, v)
			assert.Equal(t, []bool{true, false}, seen, "one call per change")

			cancel()
			require.NoError(t, q.Pause())
			assert.Len(t, seen, 2, "no call after cancel")
		})
	}
}
