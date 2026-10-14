package varmq

import (
	"sync"
	"sync/atomic"
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

// closeTracker records whether Close was called on it.
type closeTracker struct{ closed atomic.Bool }

func (c *closeTracker) Close() error {
	c.closed.Store(true)
	return nil
}

func TestTopic(t *testing.T) {
	t.Run("publish with zero subscribers", func(t *testing.T) {
		topic := NewTopic[int]()

		assert.False(t, topic.Publish(1), "nothing was delivered")
		assert.Equal(t, 0, topic.NumSubscribers())
	})

	t.Run("close is idempotent and rejects publish", func(t *testing.T) {
		topic := NewTopic[int]()

		assert.NoError(t, topic.Close())
		assert.NoError(t, topic.Close())

		assert.False(t, topic.Publish(1))
		assert.Equal(t, 0, topic.NumSubscribers())
	})

	t.Run("publish reaches every subscriber exactly once", func(t *testing.T) {
		topic := NewTopic[int]()

		var mu sync.Mutex
		got := map[string][]int{}
		record := func(name string) func(j Job[int]) {
			return func(j Job[int]) {
				mu.Lock()
				defer mu.Unlock()
				got[name] = append(got[name], j.Data())
			}
		}

		a := NewWorker(record("a"))
		b := NewWorker(record("b"), 4)
		c := NewWorker(record("c"))
		a.WithTopic(topic)
		b.WithTopic(topic)
		c.WithTopic(topic)

		assert.Equal(t, 3, topic.NumSubscribers())
		assert.True(t, topic.Publish(42))

		for _, w := range []Worker{a, b, c} {
			waitOrTimeout(t, time.Second, w.Wait)
		}

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, map[string][]int{"a": {42}, "b": {42}, "c": {42}}, got)
	})

	t.Run("subscription exposes worker and has no pending items after processing", func(t *testing.T) {
		topic := NewTopic[int]()
		w := NewWorker(func(j Job[int]) {}, WithName("topic-sub-worker"))
		sub := w.WithTopic(topic)

		assert.Equal(t, "topic-sub-worker", sub.Worker().Name())
		assert.False(t, sub.IsFull(), "no capacity in v1")

		assert.True(t, topic.Publish(1))
		waitOrTimeout(t, time.Second, w.Wait)

		assert.Equal(t, 0, sub.Len())
	})

	t.Run("late subscriber only sees later publishes", func(t *testing.T) {
		topic := NewTopic[int]()

		var mu sync.Mutex
		var late []int
		early := NewWorker(func(j Job[int]) {})
		early.WithTopic(topic)

		assert.True(t, topic.Publish(1))
		waitOrTimeout(t, time.Second, early.Wait)

		lateWorker := NewWorker(func(j Job[int]) {
			mu.Lock()
			defer mu.Unlock()
			late = append(late, j.Data())
		})
		lateWorker.WithTopic(topic)

		assert.True(t, topic.Publish(2))
		waitOrTimeout(t, time.Second, lateWorker.Wait)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, []int{2}, late)
	})

	t.Run("bound queue and topic drain under round robin", func(t *testing.T) {
		topic := NewTopic[string]()

		var mu sync.Mutex
		var seen []string
		w := NewWorker(func(j Job[string]) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, j.Data())
		}, WithStrategy(RoundRobin))

		q := w.BindQueue()
		w.WithTopic(topic)

		_, ok := q.Add("queue")
		assert.True(t, ok)
		assert.True(t, topic.Publish("topic"))

		waitOrTimeout(t, time.Second, w.Wait)

		mu.Lock()
		defer mu.Unlock()
		assert.ElementsMatch(t, []string{"queue", "topic"}, seen)
	})

	t.Run("one worker on two topics", func(t *testing.T) {
		t1 := NewTopic[int]()
		t2 := NewTopic[int]()

		var mu sync.Mutex
		var seen []int
		w := NewWorker(func(j Job[int]) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, j.Data())
		})
		w.WithTopic(t1)
		w.WithTopic(t2)

		assert.True(t, t1.Publish(1))
		assert.True(t, t2.Publish(2))
		waitOrTimeout(t, time.Second, w.Wait)

		mu.Lock()
		defer mu.Unlock()
		assert.ElementsMatch(t, []int{1, 2}, seen)
	})

	t.Run("double subscribe gives two copies", func(t *testing.T) {
		topic := NewTopic[int]()

		var count int32
		w := NewWorker(func(j Job[int]) { atomic.AddInt32(&count, 1) })
		w.WithTopic(topic)
		w.WithTopic(topic)

		assert.Equal(t, 2, topic.NumSubscribers())

		assert.True(t, topic.Publish(1))
		waitOrTimeout(t, time.Second, w.Wait)

		assert.Equal(t, int32(2), atomic.LoadInt32(&count))
	})

	t.Run("pointer payload is shared between subscribers", func(t *testing.T) {
		type box struct{ n int }
		topic := NewTopic[*box]()

		var mu sync.Mutex
		var ptrs []*box
		collect := func(j Job[*box]) {
			mu.Lock()
			defer mu.Unlock()
			ptrs = append(ptrs, j.Data())
		}
		w1 := NewWorker(collect)
		w2 := NewWorker(collect)
		w1.WithTopic(topic)
		w2.WithTopic(topic)

		b := &box{n: 1}
		assert.True(t, topic.Publish(b))
		waitOrTimeout(t, time.Second, w1.Wait)
		waitOrTimeout(t, time.Second, w2.Wait)

		mu.Lock()
		defer mu.Unlock()
		assert.Len(t, ptrs, 2)
		assert.Same(t, b, ptrs[0])
		assert.Same(t, b, ptrs[1])
	})

	t.Run("paused worker keeps copies pending until resume", func(t *testing.T) {
		topic := NewTopic[int]()
		w := NewWorker(func(j Job[int]) {})
		sub := w.WithTopic(topic)

		assert.NoError(t, w.PauseAndWait())

		assert.True(t, topic.Publish(1))
		assert.Equal(t, 1, sub.Len())

		assert.NoError(t, w.Resume())
		waitOrTimeout(t, time.Second, w.Wait)
		assert.Equal(t, 0, sub.Len())
	})

	t.Run("nil topic panics", func(t *testing.T) {
		w := NewWorker(func(j Job[int]) {})

		assert.Panics(t, func() { w.WithTopic(nil) })
	})

	t.Run("unsubscribe stops delivery and is idempotent", func(t *testing.T) {
		topic := NewTopic[int]()

		var count int32
		keep := NewWorker(func(j Job[int]) { atomic.AddInt32(&count, 1) })
		drop := NewWorker(func(j Job[int]) { t.Error("unsubscribed worker must not receive") })

		keep.WithTopic(topic)
		sub := drop.WithTopic(topic)
		assert.Equal(t, 2, topic.NumSubscribers())

		before := sub.(*subscription[int]).worker.sources.Count()
		sub.Unsubscribe()
		sub.Unsubscribe()
		assert.Equal(t, before-1, sub.(*subscription[int]).worker.sources.Count(), "internal queue unregistered from worker")
		assert.Equal(t, 1, topic.NumSubscribers())

		assert.True(t, topic.Publish(1))
		waitOrTimeout(t, time.Second, keep.Wait)
		assert.Equal(t, int32(1), atomic.LoadInt32(&count))
		assert.Equal(t, 0, drop.NumPending(), "queue was unregistered from the worker")
	})

	t.Run("unsubscribe drops pending copies", func(t *testing.T) {
		topic := NewTopic[int]()
		w := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		sub := w.WithTopic(topic)

		assert.True(t, topic.Publish(1))
		assert.Equal(t, 1, sub.Len())
		assert.Equal(t, 1, w.NumPending())

		sub.Unsubscribe()

		assert.Equal(t, 0, sub.Len())
		assert.Equal(t, 0, w.NumPending())
	})

	t.Run("purge drops pending copies but keeps the subscription", func(t *testing.T) {
		topic := NewTopic[int]()
		w := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		sub := w.WithTopic(topic)

		assert.True(t, topic.Publish(1))
		assert.Equal(t, 1, sub.Len())

		sub.Purge()

		assert.Equal(t, 0, sub.Len())
		assert.Equal(t, 1, topic.NumSubscribers(), "purge does not unsubscribe")
	})

	t.Run("purge does not close payloads", func(t *testing.T) {
		topic := NewTopic[*closeTracker]()
		sub := NewWorker(func(j Job[*closeTracker]) {}, WithAutoRun(false)).WithTopic(topic)

		payload := &closeTracker{}
		assert.True(t, topic.Publish(payload))
		assert.Equal(t, 1, sub.Len())

		sub.Purge()

		assert.Equal(t, 0, sub.Len())
		assert.False(t, payload.closed.Load(), "Purge must not call Close on user payloads")
	})

	t.Run("close detaches all subscriptions", func(t *testing.T) {
		topic := NewTopic[int]()
		w1 := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		w2 := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		w1.WithTopic(topic)
		w2.WithTopic(topic)

		assert.True(t, topic.Publish(1))
		assert.Equal(t, 1, w1.NumPending())
		assert.Equal(t, 1, w2.NumPending())

		assert.NoError(t, topic.Close())

		assert.Equal(t, 0, topic.NumSubscribers())
		assert.Equal(t, 0, w1.NumPending())
		assert.Equal(t, 0, w2.NumPending())
		assert.False(t, topic.Publish(2))
	})

	t.Run("subscribe after close returns an inert handle", func(t *testing.T) {
		topic := NewTopic[int]()
		assert.NoError(t, topic.Close())

		w := NewWorker(func(j Job[int]) { t.Error("must never run") })
		sub := w.WithTopic(topic)

		assert.NotNil(t, sub)
		assert.Equal(t, 0, topic.NumSubscribers())
		assert.Equal(t, 0, w.NumPending())
		assert.False(t, topic.Publish(1))
		assert.Equal(t, 0, sub.Len())
	})

	t.Run("close on subscription is unsubscribe", func(t *testing.T) {
		topic := NewTopic[int]()
		w := NewWorker(func(j Job[int]) { t.Error("closed subscription must not receive") })
		sub := w.WithTopic(topic)

		assert.NoError(t, sub.Close())
		assert.Equal(t, 0, topic.NumSubscribers())
		assert.False(t, topic.Publish(1))
	})

	t.Run("panicking subscriber does not affect the others", func(t *testing.T) {
		topic := NewTopic[int]()

		ok := NewWorker(func(j Job[int]) {})
		bad := NewWorker(func(j Job[int]) { panic("boom") })
		ok.WithTopic(topic)
		bad.WithTopic(topic)

		assert.True(t, topic.Publish(1))
		waitOrTimeout(t, time.Second, bad.Wait)
		waitOrTimeout(t, time.Second, ok.Wait)

		assert.Equal(t, uint64(1), bad.Metrics().Failed())
		assert.Equal(t, uint64(1), ok.Metrics().Successful())
	})

	t.Run("publish racing unsubscribe never strands a copy", func(t *testing.T) {
		topic := NewTopic[int]()
		permanent := NewWorker(func(j Job[int]) {})
		permanent.WithTopic(topic) // keeps Publish returning true

		const rounds = 200
		for i := 0; i < rounds; i++ {
			sub := NewWorker(func(j Job[int]) {}, WithAutoRun(false)).WithTopic(topic)

			done := make(chan bool, 1)
			go func() { done <- topic.Publish(i) }()
			sub.Unsubscribe()

			assert.True(t, <-done)
			assert.Equal(t, 0, sub.Len(), "round %d: copy stranded in unsubscribed queue", i)
		}

		waitOrTimeout(t, 5*time.Second, permanent.Wait)
	})

	t.Run("concurrent publish with subscribe and unsubscribe", func(t *testing.T) {
		topic := NewTopic[int]()
		defer topic.Close()

		const publishers = 4
		const perPublisher = 200
		const churn = 50

		var received int32
		permanent := NewWorker(func(j Job[int]) { atomic.AddInt32(&received, 1) })
		permanent.WithTopic(topic)

		var wg sync.WaitGroup
		var accepted int32

		for p := 0; p < publishers; p++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < perPublisher; i++ {
					if topic.Publish(i) {
						atomic.AddInt32(&accepted, 1)
					}
				}
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < churn; i++ {
				sub := NewWorker(func(j Job[int]) {}).WithTopic(topic)
				sub.Unsubscribe()
				assert.Equal(t, 0, sub.Len(), "copy stranded in churned subscription")
			}
		}()

		waitOrTimeout(t, 10*time.Second, wg.Wait)
		waitOrTimeout(t, 10*time.Second, permanent.Wait)

		assert.Equal(t, int32(publishers*perPublisher), atomic.LoadInt32(&accepted), "the permanent subscriber accepts every publish")
		assert.Equal(t, int32(publishers*perPublisher), atomic.LoadInt32(&received), "the permanent subscriber processes every copy")
		assert.Equal(t, 1, topic.NumSubscribers())
	})

	t.Run("topic view exposes payloads and closes its cursor", func(t *testing.T) {
		topic := NewTopic[int]()
		sub := NewWorker(func(j Job[int]) {}, WithAutoRun(false)).WithTopic(topic)
		assert.True(t, topic.Publish(1))

		view := sub.(*subscription[int]).view
		assert.Equal(t, []any{1}, view.Values())

		data, ok := view.Dequeue()
		assert.True(t, ok)
		assert.Equal(t, 1, data, "view hands out the raw payload, not a job")
		assert.True(t, topic.Publish(2), "refill so Close still has something to drop")

		assert.NoError(t, view.Close())
		assert.Equal(t, 0, view.Len())
		_, ok = view.Dequeue()
		assert.False(t, ok)
	})
}
