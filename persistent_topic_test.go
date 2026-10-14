package varmq

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
)

func TestPersistentTopic(t *testing.T) {
	t.Run("every group gets every payload", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)

		var a, b atomic.Int32
		wa := NewWorker(func(j Job[int]) { a.Add(int32(j.Data())) })
		wb := NewWorker(func(j Job[int]) { b.Add(int32(j.Data())) })
		wa.WithPersistentTopic(topic, "a")
		wb.WithPersistentTopic(topic, "b")
		assert.Equal(t, 2, topic.NumSubscribers())

		assert.True(t, topic.Publish(5))
		waitOrTimeout(t, time.Second, wa.Wait)
		waitOrTimeout(t, time.Second, wb.Wait)

		assert.Equal(t, int32(5), a.Load())
		assert.Equal(t, int32(5), b.Load())
	})

	t.Run("workers on one group share it", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)

		var n1, n2 atomic.Int32
		w1 := NewWorker(func(j Job[int]) { n1.Add(1) })
		w2 := NewWorker(func(j Job[int]) { n2.Add(1) })
		w1.WithPersistentTopic(topic, "jobs")
		w2.WithPersistentTopic(topic, "jobs")

		for i := 0; i < 10; i++ {
			assert.True(t, topic.Publish(i))
		}
		waitOrTimeout(t, time.Second, w1.Wait)
		waitOrTimeout(t, time.Second, w2.Wait)

		assert.Equal(t, int32(10), n1.Load()+n2.Load())
	})

	t.Run("close keeps the group's position", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)

		first := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		sub := first.WithPersistentTopic(topic, "a")
		topic.Publish(1)
		topic.Publish(2)

		before := sub.(*durableSubscription[int]).worker.numSources()
		assert.NoError(t, sub.Close())
		assert.Equal(t, before-1, sub.(*durableSubscription[int]).worker.numSources(), "source unregistered from worker")
		assert.Equal(t, 0, topic.NumSubscribers())
		assert.Equal(t, 0, first.NumPending())
		assert.Equal(t, 2, storage.GroupLen("a"))

		var mu sync.Mutex
		var got []int
		second := NewWorker(func(j Job[int]) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, j.Data())
		})
		second.WithPersistentTopic(topic, "a")
		waitOrTimeout(t, time.Second, second.Wait)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, []int{1, 2}, got)
	})

	t.Run("unsubscribe deletes the group", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)
		sub := NewWorker(func(j Job[int]) {}).WithPersistentTopic(topic, "a")

		sub.Unsubscribe()
		sub.Unsubscribe()

		assert.False(t, storage.HasGroup("a"))
		assert.Equal(t, 0, topic.NumSubscribers())
	})

	t.Run("completed jobs are acknowledged", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)
		w := NewWorker(func(j Job[int]) {})
		w.WithPersistentTopic(topic, "a")

		topic.Publish(1)
		waitOrTimeout(t, time.Second, w.Wait)

		assert.Equal(t, []string{"a:1"}, storage.AckedIDs())
	})

	t.Run("job id survives publish", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)

		var id atomic.Value
		w := NewWorker(func(j Job[int]) { id.Store(j.ID()) })
		w.WithPersistentTopic(topic, "a")

		assert.True(t, topic.Publish(1, WithJobId("order-1")))
		waitOrTimeout(t, time.Second, w.Wait)

		assert.Equal(t, "order-1", id.Load())
	})

	t.Run("purge skips the group's backlog", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)
		sub := NewWorker(func(j Job[int]) {}, WithAutoRun(false)).WithPersistentTopic(topic, "a")
		topic.Publish(1)
		topic.Publish(2)

		sub.Purge()

		assert.Equal(t, 0, storage.GroupLen("a"))
		assert.Equal(t, 1, topic.NumSubscribers())
	})

	t.Run("storage publish failure returns false", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)
		storage.ShouldFailPublish = true

		assert.False(t, topic.Publish(1))
	})

	t.Run("join failure gives an inert handle and reports the error", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		storage.ShouldFailJoin = true
		topic := NewPersistentTopic[int](storage)

		var reported atomic.Bool
		w := NewWorker(func(j Job[int]) {}, WithErrHandler(func(Worker, error) { reported.Store(true) }))
		sub := w.WithPersistentTopic(topic, "a")

		assert.True(t, reported.Load())
		assert.Equal(t, 0, topic.NumSubscribers())
		assert.Equal(t, 0, w.NumPending())
		sub.Unsubscribe()
	})

	t.Run("inert handle after failed join never touches a live group", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)

		// group "a" exists from before a restart and holds one message
		assert.NoError(t, storage.JoinGroup("a"))
		assert.True(t, topic.Publish(1))
		storage.ShouldFailJoin = true

		var reported atomic.Bool
		w := NewWorker(func(j Job[int]) {}, WithErrHandler(func(Worker, error) { reported.Store(true) }))
		sub := w.WithPersistentTopic(topic, "a")

		assert.True(t, reported.Load())
		assert.Equal(t, 0, sub.Len(), "inert handle reports no backlog")
		sub.Purge()
		assert.Equal(t, 1, storage.GroupLen("a"), "inert Purge must not skip the live group")
		sub.Unsubscribe()
		assert.True(t, storage.HasGroup("a"), "inert Unsubscribe must not delete the live group")
	})

	t.Run("join failure error handler may use the topic", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		storage.ShouldFailJoin = true
		topic := NewPersistentTopic[int](storage)

		var reported atomic.Bool
		var seenSubs atomic.Int32
		w := NewWorker(func(j Job[int]) {}, WithErrHandler(func(Worker, error) {
			seenSubs.Store(int32(topic.NumSubscribers()))
			reported.Store(true)
		}))

		var sub Subscription[int]
		waitOrTimeout(t, time.Second, func() {
			sub = w.WithPersistentTopic(topic, "a")
		})

		assert.True(t, reported.Load())
		assert.Equal(t, int32(0), seenSubs.Load())
		sub.Unsubscribe()
	})

	t.Run("closed topic rejects publish and binds inert", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)
		assert.NoError(t, topic.Close())
		assert.NoError(t, topic.Close())

		assert.False(t, topic.Publish(1))

		sub := NewWorker(func(j Job[int]) {}).WithPersistentTopic(topic, "a")
		assert.False(t, storage.HasGroup("a"))
		sub.Unsubscribe()
	})

	t.Run("inert handle on a closed topic never touches storage", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)

		assert.NoError(t, storage.JoinGroup("a"))
		assert.True(t, topic.Publish(1))
		assert.NoError(t, topic.Close())

		sub := NewWorker(func(j Job[int]) {}).WithPersistentTopic(topic, "a")
		assert.Equal(t, 0, sub.Len())
		sub.Purge()
		assert.Equal(t, 1, storage.GroupLen("a"))
		sub.Unsubscribe()
		assert.True(t, storage.HasGroup("a"))
	})

	t.Run("unsubscribe detaches the group's siblings", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)

		w1 := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		w2 := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		sub1 := w1.WithPersistentTopic(topic, "jobs")
		w2.WithPersistentTopic(topic, "jobs")
		assert.Equal(t, 2, topic.NumSubscribers())

		topic.Publish(1)
		assert.Equal(t, 1, w1.NumPending())
		assert.Equal(t, 1, w2.NumPending())

		sub1.Unsubscribe()

		assert.Equal(t, 0, topic.NumSubscribers())
		assert.False(t, storage.HasGroup("jobs"))
		assert.Equal(t, 0, w1.NumPending())
		assert.Equal(t, 0, w2.NumPending())
	})

	t.Run("ack happens after the handler returns", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)

		release := make(chan struct{})
		w := NewWorker(func(j Job[int]) { <-release })
		w.WithPersistentTopic(topic, "a")

		assert.True(t, topic.Publish(1))
		assert.Eventually(t, func() bool { return storage.PendingCount("a") == 1 }, time.Second, time.Millisecond)
		assert.Empty(t, storage.AckedIDs())

		close(release)
		waitOrTimeout(t, time.Second, w.Wait)

		assert.Equal(t, []string{"a:1"}, storage.AckedIDs())
		assert.Equal(t, 0, storage.PendingCount("a"))
	})

	t.Run("ack failure reaches the error handler", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		storage.ShouldFailAcknowledge = true
		topic := NewPersistentTopic[int](storage)

		var mu sync.Mutex
		var errs []error
		w := NewWorker(func(j Job[int]) {}, WithErrHandler(func(_ Worker, err error) {
			mu.Lock()
			defer mu.Unlock()
			errs = append(errs, err)
		}))
		w.WithPersistentTopic(topic, "a")

		assert.True(t, topic.Publish(1))
		waitOrTimeout(t, time.Second, w.Wait)

		assert.Eventually(t, func() bool {
			mu.Lock()
			defer mu.Unlock()
			for _, err := range errs {
				if errors.Is(err, ErrAcknowledgeJob) {
					return true
				}
			}
			return false
		}, time.Second, time.Millisecond)
	})

	t.Run("rebinding after unsubscribe starts at the head", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)

		subA := NewWorker(func(j Job[int]) {}, WithAutoRun(false)).WithPersistentTopic(topic, "a")
		assert.True(t, topic.Publish(1))
		assert.True(t, topic.Publish(2))
		subA.Unsubscribe()
		assert.True(t, topic.Publish(3))

		var mu sync.Mutex
		var got []int
		b := NewWorker(func(j Job[int]) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, j.Data())
		})
		b.WithPersistentTopic(topic, "a")
		assert.True(t, topic.Publish(4))
		waitOrTimeout(t, time.Second, b.Wait)

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, []int{4}, got)
	})

	t.Run("stale sibling cannot delete a newer group", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		topic := NewPersistentTopic[int](storage)

		s1 := NewWorker(func(j Job[int]) {}, WithAutoRun(false)).WithPersistentTopic(topic, "g")
		s2 := NewWorker(func(j Job[int]) {}, WithAutoRun(false)).WithPersistentTopic(topic, "g")

		s1.Unsubscribe()
		assert.False(t, storage.HasGroup("g"))

		NewWorker(func(j Job[int]) {}, WithAutoRun(false)).WithPersistentTopic(topic, "g")
		assert.True(t, storage.HasGroup("g"))

		s2.Unsubscribe()

		assert.True(t, storage.HasGroup("g"))
		assert.Equal(t, 1, topic.NumSubscribers())
	})

	t.Run("guards", func(t *testing.T) {
		w := NewWorker(func(j Job[int]) {})
		topic := NewPersistentTopic[int](mocks.NewMockPersistentTopic())

		assert.Panics(t, func() { NewPersistentTopic[int](nil) })
		assert.Panics(t, func() { w.WithPersistentTopic(nil, "a") })
		assert.Panics(t, func() { w.WithPersistentTopic(topic, "") })
	})

	t.Run("group view adapts one group", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		assert.NoError(t, storage.JoinGroup("g"))
		storage.Publish([]byte("x"))
		storage.Publish([]byte("y"))

		v := &groupView{storage: storage, group: "g"}
		assert.Equal(t, 2, v.Len())
		assert.Nil(t, v.Values(), "never reads storage")

		item, ok := v.Dequeue()
		assert.True(t, ok)
		assert.Equal(t, []byte("x"), item)

		item, ok, ack := v.DequeueWithAckId()
		assert.True(t, ok)
		assert.Equal(t, []byte("y"), item)
		assert.True(t, v.Acknowledge(ack))

		storage.Publish([]byte("z"))
		v.Purge()
		assert.Equal(t, 0, v.Len())
		assert.NoError(t, v.Close())
	})

	t.Run("inert group view never touches storage", func(t *testing.T) {
		storage := mocks.NewMockPersistentTopic()
		assert.NoError(t, storage.JoinGroup("g"))
		storage.Publish([]byte("x"))

		v := &groupView{storage: storage, group: "g", inert: true}
		assert.Equal(t, 0, v.Len())
		_, ok := v.Dequeue()
		assert.False(t, ok)
		_, ok, _ = v.DequeueWithAckId()
		assert.False(t, ok)
		assert.False(t, v.Acknowledge("1"))
		v.Purge()

		assert.Equal(t, 1, storage.GroupLen("g"))
		assert.Equal(t, 0, storage.PendingCount("g"))
	})

	t.Run("publish fails when the payload cannot be serialized", func(t *testing.T) {
		topic := NewPersistentTopic[chan int](mocks.NewMockPersistentTopic())

		assert.False(t, topic.Publish(make(chan int)))
	})
}
