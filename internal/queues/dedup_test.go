package queues

import (
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
					q.idMu.Lock()
					defer q.idMu.Unlock()
					return len(q.ids)
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

// The id set must not move Queue out of its 128-byte size class, whose
// objects start on a cache line (see Queue), nor PriorityQueue out of 64.
func TestQueueSizeClass(t *testing.T) {
	assert.Equal(t, uintptr(128), unsafe.Sizeof(Queue[int]{}))
	assert.LessOrEqual(t, unsafe.Sizeof(PriorityQueue[int]{}), uintptr(64))
}
