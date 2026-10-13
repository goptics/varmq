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

		NewWorker(record("a")).WithTopic(topic)
		NewWorker(record("b"), 4).WithTopic(topic)
		NewWorker(record("c")).WithTopic(topic)

		assert.Equal(t, 3, topic.NumSubscribers())

		gj, ok := topic.Publish(42)

		assert.True(t, ok)
		waitOrTimeout(t, time.Second, gj.Wait)
		assert.Equal(t, 0, gj.NumPending())

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

		gj, _ := topic.Publish(1)
		waitOrTimeout(t, time.Second, gj.Wait)

		assert.Equal(t, 0, sub.Len())
	})

	t.Run("late subscriber only sees later publishes", func(t *testing.T) {
		topic := NewTopic[int]()

		var mu sync.Mutex
		var late []int
		NewWorker(func(j Job[int]) {}).WithTopic(topic)

		gj1, ok := topic.Publish(1)
		assert.True(t, ok)
		waitOrTimeout(t, time.Second, gj1.Wait)

		NewWorker(func(j Job[int]) {
			mu.Lock()
			defer mu.Unlock()
			late = append(late, j.Data())
		}).WithTopic(topic)

		gj2, ok := topic.Publish(2)
		assert.True(t, ok)
		waitOrTimeout(t, time.Second, gj2.Wait)

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

		j, ok := q.Add("queue")
		assert.True(t, ok)
		gj, ok := topic.Publish("topic")
		assert.True(t, ok)

		waitOrTimeout(t, time.Second, j.Wait)
		waitOrTimeout(t, time.Second, gj.Wait)

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

		gj1, _ := t1.Publish(1)
		gj2, _ := t2.Publish(2)
		waitOrTimeout(t, time.Second, gj1.Wait)
		waitOrTimeout(t, time.Second, gj2.Wait)

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

		gj, _ := topic.Publish(1)
		waitOrTimeout(t, time.Second, gj.Wait)

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
		NewWorker(collect).WithTopic(topic)
		NewWorker(collect).WithTopic(topic)

		b := &box{n: 1}
		gj, _ := topic.Publish(b)
		waitOrTimeout(t, time.Second, gj.Wait)

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

		gj, ok := topic.Publish(1)
		assert.True(t, ok)
		assert.Equal(t, 1, gj.NumPending())
		assert.Equal(t, 1, sub.Len())

		assert.NoError(t, w.Resume())
		waitOrTimeout(t, time.Second, gj.Wait)
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

		before := sub.(*subscription[int]).worker.queues.Count()
		sub.Unsubscribe()
		sub.Unsubscribe()
		assert.Equal(t, before-1, sub.(*subscription[int]).worker.queues.Count(), "internal queue unregistered from worker")
		assert.Equal(t, 1, topic.NumSubscribers())

		gj, ok := topic.Publish(1)
		assert.True(t, ok)
		waitOrTimeout(t, time.Second, gj.Wait)
		assert.Equal(t, int32(1), atomic.LoadInt32(&count))
		assert.Equal(t, 0, drop.NumPending(), "queue was unregistered from the worker")
	})

	t.Run("unsubscribe releases waiters on pending copies", func(t *testing.T) {
		topic := NewTopic[int]()
		w := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		sub := w.WithTopic(topic)

		gj, ok := topic.Publish(1)
		assert.True(t, ok)
		assert.Equal(t, 1, gj.NumPending())
		assert.Equal(t, 1, sub.Len())

		sub.Unsubscribe()

		waitOrTimeout(t, time.Second, gj.Wait)
		assert.Equal(t, 0, gj.NumPending())
		assert.Equal(t, 0, sub.Len())
		assert.Equal(t, 0, w.NumPending())
	})

	t.Run("purge releases waiters on pending copies", func(t *testing.T) {
		topic := NewTopic[int]()
		w := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		sub := w.WithTopic(topic)

		gj, _ := topic.Publish(1)
		assert.Equal(t, 1, gj.NumPending())

		sub.Purge()

		waitOrTimeout(t, time.Second, gj.Wait)
		assert.Equal(t, 0, sub.Len())
		assert.Equal(t, 1, topic.NumSubscribers(), "purge does not unsubscribe")
	})

	t.Run("close detaches all subscriptions", func(t *testing.T) {
		topic := NewTopic[int]()
		w1 := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		w2 := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		w1.WithTopic(topic)
		w2.WithTopic(topic)

		gj, _ := topic.Publish(1)
		assert.Equal(t, 2, gj.NumPending())

		assert.NoError(t, topic.Close())

		assert.Equal(t, 0, topic.NumSubscribers())
		waitOrTimeout(t, time.Second, gj.Wait)
		assert.Equal(t, 0, w1.NumPending())
		assert.Equal(t, 0, w2.NumPending())

		gj2, ok := topic.Publish(2)
		assert.False(t, ok)
		assert.Nil(t, gj2)
	})

	t.Run("subscribe after close returns an inert handle", func(t *testing.T) {
		topic := NewTopic[int]()
		assert.NoError(t, topic.Close())

		w := NewWorker(func(j Job[int]) { t.Error("must never run") })
		sub := w.WithTopic(topic)

		assert.NotNil(t, sub)
		assert.Equal(t, 0, topic.NumSubscribers())
		assert.Equal(t, 0, w.NumPending())

		gj, ok := topic.Publish(1)
		assert.False(t, ok)
		assert.Nil(t, gj)
	})

	t.Run("panicking subscriber still completes the group", func(t *testing.T) {
		topic := NewTopic[int]()

		ok := NewWorker(func(j Job[int]) {})
		bad := NewWorker(func(j Job[int]) { panic("boom") })
		ok.WithTopic(topic)
		bad.WithTopic(topic)

		gj, delivered := topic.Publish(1)
		assert.True(t, delivered)
		waitOrTimeout(t, time.Second, gj.Wait)

		bad.Wait()
		assert.Equal(t, uint64(1), bad.Metrics().Failed())
		assert.Equal(t, uint64(1), ok.Metrics().Successful())
	})

	t.Run("close on subscription is unsubscribe", func(t *testing.T) {
		topic := NewTopic[int]()
		w := NewWorker(func(j Job[int]) { t.Error("closed subscription must not receive") })
		sub := w.WithTopic(topic)

		assert.NoError(t, sub.Close())
		assert.Equal(t, 0, topic.NumSubscribers())

		_, ok := topic.Publish(1)
		assert.False(t, ok)
	})

	t.Run("publish racing unsubscribe never strands a copy", func(t *testing.T) {
		topic := NewTopic[int]()
		NewWorker(func(j Job[int]) {}).WithTopic(topic) // permanent, keeps ok=true

		const rounds = 200
		for i := 0; i < rounds; i++ {
			sub := NewWorker(func(j Job[int]) {}, WithAutoRun(false)).WithTopic(topic)

			done := make(chan EnqueuedGroupJob, 1)
			go func() {
				gj, _ := topic.Publish(i)
				done <- gj
			}()
			sub.Unsubscribe()

			gj := <-done
			waitOrTimeout(t, 2*time.Second, gj.Wait)
			assert.Equal(t, 0, sub.Len(), "round %d: copy stranded in unsubscribed queue", i)
		}
	})

	t.Run("concurrent publish with subscribe and unsubscribe", func(t *testing.T) {
		topic := NewTopic[int]()
		defer topic.Close()

		const publishers = 4
		const perPublisher = 200
		const churn = 50

		NewWorker(func(j Job[int]) {}).WithTopic(topic)

		var wg sync.WaitGroup
		var groups []EnqueuedGroupJob
		var gmu sync.Mutex

		for p := 0; p < publishers; p++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < perPublisher; i++ {
					if gj, ok := topic.Publish(i); ok {
						gmu.Lock()
						groups = append(groups, gj)
						gmu.Unlock()
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
				assert.Equal(t, 0, sub.Len(), "copy stranded in unsubscribed queue")
			}
		}()

		waitOrTimeout(t, 10*time.Second, wg.Wait)

		gmu.Lock()
		defer gmu.Unlock()
		assert.Equal(t, publishers*perPublisher, len(groups), "the permanent subscriber accepts every publish")
		for _, gj := range groups {
			waitOrTimeout(t, 5*time.Second, gj.Wait)
		}
		assert.Equal(t, 1, topic.NumSubscribers())
	})
}
