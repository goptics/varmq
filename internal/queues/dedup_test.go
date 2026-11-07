package queues

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// idItem is an item whose ID is itself, as a varmq job returns its id.
type idItem string

func (i idItem) ID() string { return string(i) }

// dedupStore drives Queue and PriorityQueue through one set of tests.
type dedupStore struct {
	once    func(item idItem) bool // EnqueueOnce under the item's own id
	plain   func(item idItem) bool
	dequeue func() (any, bool)
	purge   func()
	close   func()
	setCap  func(int)
	held    func() int // ids in the set
	len     func() int
}

func dedupStores() map[string]func() dedupStore {
	return map[string]func() dedupStore{
		"queue": func() dedupStore {
			q := NewQueue[idItem]()
			return dedupStore{
				once:    func(i idItem) bool { return q.EnqueueOnce(i, string(i)) },
				plain:   func(i idItem) bool { return q.Enqueue(i) },
				dequeue: q.Dequeue,
				purge:   q.Purge,
				close:   func() { _ = q.Close() },
				setCap:  q.SetCapacity,
				held: func() int {
					if ids := q.ids.Load(); ids != nil {
						return ids.len()
					}
					return 0
				},
				len: q.Len,
			}
		},
		"priority": func() dedupStore {
			q := NewPriorityQueue[idItem]()
			return dedupStore{
				once:    func(i idItem) bool { return q.EnqueueOnce(i, 0, string(i)) },
				plain:   func(i idItem) bool { return q.Enqueue(i, 0) },
				dequeue: q.Dequeue,
				purge:   q.Purge,
				close:   func() { _ = q.Close() },
				setCap:  q.SetCapacity,
				held: func() int {
					q.mx.RLock()
					defer q.mx.RUnlock()
					return len(q.ids)
				},
				len: q.Len,
			}
		},
	}
}

func TestEnqueueOnce(t *testing.T) {
	for name, newStore := range dedupStores() {
		t.Run(name, func(t *testing.T) {
			t.Run("a repeat is refused while the item is queued", func(t *testing.T) {
				s := newStore()
				assert.True(t, s.once("a"))
				assert.False(t, s.once("a"))
				assert.True(t, s.once("b"))
				assert.Equal(t, 2, s.len())
			})

			t.Run("dequeue frees the id", func(t *testing.T) {
				s := newStore()
				s.once("a")
				v, ok := s.dequeue()
				assert.True(t, ok)
				assert.Equal(t, idItem("a"), v)
				assert.True(t, s.once("a"))
				assert.Equal(t, 1, s.held())
			})

			t.Run("purge frees every id", func(t *testing.T) {
				s := newStore()
				s.once("a")
				s.once("b")
				s.purge()
				assert.Equal(t, 0, s.held())
				assert.True(t, s.once("a"))
			})

			t.Run("a refused item keeps no id", func(t *testing.T) {
				s := newStore()
				s.setCap(1)
				assert.True(t, s.once("b"))
				assert.False(t, s.once("a"), "full")
				assert.Equal(t, 1, s.held())

				s.dequeue()
				assert.True(t, s.once("a"))

				s.dequeue()
				s.close()
				assert.False(t, s.once("c"), "closed")
				assert.Equal(t, 0, s.held())
			})

			t.Run("plain items are never deduplicated", func(t *testing.T) {
				s := newStore()
				s.once("a")
				assert.True(t, s.plain(""))
				assert.True(t, s.plain(""))
				for range 3 {
					s.dequeue()
				}
				assert.Equal(t, 0, s.held())
			})

			t.Run("concurrent repeats store one item", func(t *testing.T) {
				s := newStore()
				var stored atomic.Int32
				var wg sync.WaitGroup
				for range 64 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if s.once("a") {
							stored.Add(1)
						}
					}()
				}
				wg.Wait()
				assert.Equal(t, int32(1), stored.Load())
				assert.Equal(t, 1, s.len())
			})

			// A dequeue racing the first EnqueueOnce of a fresh queue, or one
			// landing between an Append and its insert, must still free the
			// id once the item is out.
			t.Run("no id is left behind by a racing dequeue", func(t *testing.T) {
				for range 20000 {
					s := newStore()
					done := make(chan struct{})
					go func() {
						defer close(done)
						for {
							if _, ok := s.dequeue(); ok {
								return
							}
						}
					}()
					s.once("a")
					<-done
					if !assert.Equal(t, 0, s.held()) {
						return
					}
				}
			})

			// Purge racing an EnqueueOnce must leave the set matching the
			// queue: no id without its item, which would refuse it forever.
			t.Run("purge racing an add keeps the set in step", func(t *testing.T) {
				for range 20000 {
					s := newStore()
					done := make(chan struct{})
					go func() {
						defer close(done)
						s.purge()
					}()
					s.once("a")
					<-done
					if !assert.Equal(t, s.len(), s.held()) {
						return
					}
				}
			})
		})
	}
}

func TestPriorityEnqueueOnceKeepsOrder(t *testing.T) {
	q := NewPriorityQueue[idItem]()
	q.EnqueueOnce(idItem("low"), 2, "low")
	q.EnqueueOnce(idItem("high"), 1, "high")
	v, _ := q.Dequeue()
	assert.Equal(t, idItem("high"), v)
}

// The hot fields fill Queue's first 128 bytes and the id set pointer sits
// after them (see Queue); PriorityQueue stays in class 64, so its lock never
// straddles two lines.
func TestQueueSizeClass(t *testing.T) {
	var q Queue[int]
	assert.Equal(t, uintptr(144), unsafe.Sizeof(q))
	assert.Less(t, unsafe.Offsetof(q.pauseState), uintptr(128), "the pause is in the hot block's padding")
	assert.GreaterOrEqual(t, unsafe.Offsetof(q.ids), uintptr(128), "the id set is past the hot 128-byte block")
	assert.Equal(t, uintptr(64), unsafe.Sizeof(idShard{}), "a shard fills one cache line")
	assert.LessOrEqual(t, unsafe.Sizeof(PriorityQueue[int]{}), uintptr(64))
}

// A shard set that held more than idShardShrinkPeak ids is dropped once
// drained, so a burst does not pin its peak memory; a small one is kept.
func TestIdMapDropsDrainedBurst(t *testing.T) {
	var s idMap
	assert.True(t, s.add("a"))
	assert.False(t, s.add("a"), "repeat")
	s.remove("a")
	assert.NotNil(t, s.m, "small: kept")

	for i := range idShardShrinkPeak + 1 {
		s.add(strconv.Itoa(i))
	}
	for i := range idShardShrinkPeak {
		s.remove(strconv.Itoa(i))
	}
	assert.NotNil(t, s.m, "not drained yet")

	s.remove(strconv.Itoa(idShardShrinkPeak))
	assert.Nil(t, s.m)
	assert.Zero(t, s.peak)
	assert.True(t, s.add("a"), "a dropped set is made again")
}

// Draining queues leaves no ids, and a drained id can be added again.
func TestDrainedIdSetIsEmpty(t *testing.T) {
	q := NewQueue[idItem]()
	pq := NewPriorityQueue[idItem]()

	for i := range idsShrinkPeak + 1 {
		id := strconv.Itoa(i)
		q.EnqueueOnce(idItem(id), id)
		pq.EnqueueOnce(idItem(id), 0, id)
	}
	for range idsShrinkPeak + 1 {
		q.Dequeue()
		pq.Dequeue()
	}
	ids := q.ids.Load()
	assert.Zero(t, ids.len())
	for i := range ids.shards {
		assert.LessOrEqual(t, ids.shards[i].ids.peak, idShardShrinkPeak, "drained shard keeps burst peak")
	}
	assert.Nil(t, pq.ids, "a burst's set is dropped once drained")
	assert.True(t, q.EnqueueOnce(idItem("a"), "a"))
	assert.True(t, pq.EnqueueOnce(idItem("a"), 0, "a"))
}

// EnqueueOnce refuses an item that is not a T, and an item without an ID
// method dequeues cleanly once ids are in use.
func TestEnqueueOnceOddItems(t *testing.T) {
	assert.False(t, NewQueue[int]().EnqueueOnce("x", "a"), "not a T")
	assert.False(t, NewPriorityQueue[int]().EnqueueOnce("x", 0, "a"), "not a T")

	q := NewQueue[any]()
	assert.True(t, q.EnqueueOnce(idItem("a"), "a"))
	assert.True(t, q.Enqueue(42))
	v, ok := q.Dequeue()
	assert.True(t, ok)
	assert.Equal(t, idItem("a"), v)
	v, ok = q.Dequeue()
	assert.True(t, ok)
	assert.Equal(t, 42, v)
	assert.True(t, q.EnqueueOnce(idItem("a"), "a"), "the queue still works")

	pq := NewPriorityQueue[any]()
	assert.True(t, pq.EnqueueOnce(idItem("a"), 1, "a"))
	assert.True(t, pq.Enqueue(42, 0))
	v, _ = pq.Dequeue()
	assert.Equal(t, 42, v, "an item without an ID dequeues while ids are held")
	assert.False(t, pq.EnqueueOnce(idItem("a"), 1, "a"), "a is still held")
	v, _ = pq.Dequeue()
	assert.Equal(t, idItem("a"), v)
}
