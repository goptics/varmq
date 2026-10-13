package varmq

import (
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

		assert.NoError(t, sub.Close())
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

	t.Run("guards", func(t *testing.T) {
		w := NewWorker(func(j Job[int]) {})
		topic := NewPersistentTopic[int](mocks.NewMockPersistentTopic())

		assert.Panics(t, func() { NewPersistentTopic[int](nil) })
		assert.Panics(t, func() { w.WithPersistentTopic(nil, "a") })
		assert.Panics(t, func() { w.WithPersistentTopic(topic, "") })
	})
}
