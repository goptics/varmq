package varmq

import (
	"math"
	"testing"

	"github.com/goptics/varmq/internal/queues"
	"github.com/stretchr/testify/assert"
)

func newTestSource(items ...string) *queueSource[string, iJob[string]] {
	q := queues.NewQueue[any]()
	for _, item := range items {
		q.Enqueue(newJob(item, jobConfigs{Id: item}))
	}

	return &queueSource[string, iJob[string]]{q: q}
}

func TestSourceManagerNext(t *testing.T) {
	t.Run("Strategy: RoundRobin", func(t *testing.T) {
		sm := newStrategyManager[iJob[string]](RoundRobin)

		s1 := newTestSource("s1-item")
		s2 := newTestSource("s2-item1", "s2-item2")
		s3 := newTestSource("s3-item")

		sm.Register(s1, math.MaxInt)
		sm.Register(s2, math.MaxInt)
		sm.Register(s3, math.MaxInt)

		first, ok := sm.next()
		assert.True(t, ok)
		second, ok := sm.next()
		assert.True(t, ok)
		third, ok := sm.next()
		assert.True(t, ok)
		fourth, ok := sm.next()
		assert.True(t, ok)

		assert.Same(t, first, fourth, "round-robin cycles back to the first source")
		assert.NotSame(t, first, second)
		assert.NotSame(t, second, third)
	})

	t.Run("Strategy: MaxLen", func(t *testing.T) {
		sm := newStrategyManager[iJob[string]](MaxLen)

		s1 := newTestSource("a")
		s2 := newTestSource("a", "b")
		s3 := newTestSource("a", "b", "c")

		sm.Register(s1, math.MaxInt)
		sm.Register(s2, math.MaxInt)
		sm.Register(s3, math.MaxInt)

		src, ok := sm.next()
		assert.True(t, ok)
		assert.Equal(t, 3, src.Len())
		assert.Same(t, s3, src)
	})

	t.Run("Strategy: MinLen", func(t *testing.T) {
		sm := newStrategyManager[iJob[string]](MinLen)

		s1 := newTestSource("a")
		s2 := newTestSource("a", "b")
		s3 := newTestSource("a", "b", "c")

		sm.Register(s1, math.MaxInt)
		sm.Register(s2, math.MaxInt)
		sm.Register(s3, math.MaxInt)

		src, ok := sm.next()
		assert.True(t, ok)
		assert.Equal(t, 1, src.Len())
		assert.Same(t, s1, src)
	})

	t.Run("Strategy: Priority", func(t *testing.T) {
		sm := newStrategyManager[iJob[string]](Priority)

		s1 := newTestSource("a")
		s2 := newTestSource("a", "b")
		s3 := newTestSource("a")

		// Lower number = higher priority
		sm.Register(s1, 10)
		sm.Register(s2, 5)
		sm.Register(s3, 1)

		src, ok := sm.next()
		assert.True(t, ok)
		assert.Same(t, s3, src, "highest priority source with items comes first")

		sm.UnregisterItem(s3)

		src, ok = sm.next()
		assert.True(t, ok)
		assert.Same(t, s2, src, "next highest priority after s3 is unregistered")
	})

	t.Run("No Sources Registered", func(t *testing.T) {
		for _, strategy := range []Strategy{RoundRobin, MaxLen, MinLen, Priority} {
			sm := newStrategyManager[iJob[string]](strategy)
			src, ok := sm.next()
			assert.False(t, ok)
			assert.Nil(t, src)
		}
	})

	t.Run("Invalid Strategy", func(t *testing.T) {
		// An out-of-range strategy falls back to the default (Priority).
		sm := newStrategyManager[iJob[string]](Strategy(99))
		sm.Register(newTestSource(), math.MaxInt)

		src, ok := sm.next()
		assert.False(t, ok, "empty source is skipped by the Priority fallback")
		assert.Nil(t, src)
	})
}
