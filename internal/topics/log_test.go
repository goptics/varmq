package topics

import (
	"runtime"
	"sync"
	"testing"
	"time"
	"weak"

	"github.com/stretchr/testify/assert"
)

func drain[T any](c *Cursor[T]) []T {
	var out []T
	for {
		v, ok := c.Dequeue()
		if !ok {
			return out
		}
		out = append(out, v)
	}
}

func TestLog(t *testing.T) {
	t.Run("every cursor reads every item at its own pace", func(t *testing.T) {
		l := NewLog[int]()
		a, b := l.NewCursor(), l.NewCursor()

		for i := 1; i <= 3; i++ {
			assert.Equal(t, 2, l.Append(i))
		}

		assert.Equal(t, []int{1, 2, 3}, drain(a))
		assert.Equal(t, 0, a.Len())
		assert.Equal(t, 3, b.Len())

		v, ok := b.Dequeue()
		assert.True(t, ok)
		assert.Equal(t, 1, v)
		assert.Equal(t, []int{2, 3}, b.Values())
		assert.Equal(t, 2, b.Len(), "Values does not consume")
	})

	t.Run("new cursor starts at the head", func(t *testing.T) {
		l := NewLog[int]()
		a := l.NewCursor()
		l.Append(1)
		b := l.NewCursor()
		l.Append(2)

		assert.Equal(t, []int{2}, drain(b))
		assert.Equal(t, []int{1, 2}, drain(a))
	})

	t.Run("purge skips to the head", func(t *testing.T) {
		l := NewLog[int]()
		c := l.NewCursor()
		l.Append(1)
		l.Append(2)

		c.Purge()
		assert.Equal(t, 0, c.Len())

		l.Append(3)
		assert.Equal(t, []int{3}, drain(c))
	})

	t.Run("closed cursor reads nothing and stops counting", func(t *testing.T) {
		l := NewLog[int]()
		a, b := l.NewCursor(), l.NewCursor()
		assert.Equal(t, 2, l.Append(1))

		a.Close()
		a.Close()

		assert.Equal(t, 0, a.Len())
		_, ok := a.Dequeue()
		assert.False(t, ok)
		assert.Empty(t, a.Values())
		assert.Equal(t, 1, l.Append(2))
		assert.Equal(t, []int{1, 2}, drain(b))
	})

	t.Run("a limit refuses appends while the slowest cursor is that far behind", func(t *testing.T) {
		l := NewBoundedLog[int](2)
		fast, slow := l.NewCursor(), l.NewCursor()

		assert.Equal(t, 2, l.Append(1))
		assert.Equal(t, 2, l.Append(2))
		assert.True(t, l.Full())
		assert.Equal(t, 0, l.Append(3), "slow has 2 unread")

		drain(fast)
		assert.Equal(t, 0, l.Append(3), "the slowest cursor decides, not the fastest")

		_, _ = slow.Dequeue()
		assert.False(t, l.Full())
		assert.Equal(t, 2, l.Append(3), "reading frees room")
		assert.Equal(t, 0, l.Append(4))

		slow.Purge()
		drain(fast)
		assert.Equal(t, 2, l.Append(4), "purge frees room")
		assert.Equal(t, 2, l.Append(5))
		assert.Equal(t, 0, l.Append(6))

		drain(fast)
		assert.Equal(t, 0, l.Append(6), "slow still has 2 unread")
		slow.Close()
		assert.Equal(t, 1, l.Append(6), "closing the slow cursor frees room")
		assert.Equal(t, 1, l.Append(7))
		assert.Equal(t, 0, l.Append(8), "now fast is the slowest, 2 behind")
		assert.Equal(t, []int{6, 7}, drain(fast))
	})

	t.Run("limit 0 is unbounded", func(t *testing.T) {
		l := NewBoundedLog[int](0)
		assert.False(t, l.Full(), "no cursors")
		l.NewCursor()
		for i := range 5000 {
			assert.Equal(t, 1, l.Append(i))
		}
		assert.False(t, l.Full())
	})

	t.Run("bounded append waiting for the lock observes a close made meanwhile", func(t *testing.T) {
		l := NewBoundedLog[int](10)
		c := l.NewCursor()
		l.mx.Lock()

		res := make(chan int, 1)
		go func() { res <- l.Append(1) }()

		time.Sleep(50 * time.Millisecond)
		l.Close()
		l.mx.Unlock()

		assert.Equal(t, 0, <-res)
		assert.Equal(t, 0, c.Len())
	})

	t.Run("a consumer clears each slot it reads and purge skips the rest", func(t *testing.T) {
		l := NewLog[*int]()
		c := l.NewConsumer()
		one, two := 1, 2
		l.Append(&one)
		l.Append(&two)

		v, ok := c.Dequeue()
		assert.True(t, ok)
		assert.Same(t, &one, v)
		assert.Nil(t, c.chunk.data[0], "read slot cleared, so the item can be collected")
		assert.Same(t, &two, c.chunk.data[1], "unread slot kept")

		big := new([1 << 20]int) // only the slot will hold it
		purged := weak.Make(big)
		l.Append(&big[0])
		big = nil
		c.Purge()
		assert.Equal(t, 0, c.Len())
		runtime.GC()
		assert.Nil(t, purged.Value(), "purge clears the slots it skips, so they are freed at once")
		l.Append(&one)
		v, _ = c.Dequeue()
		assert.Same(t, &one, v)
	})

	t.Run("set limit bounds an existing log", func(t *testing.T) {
		l := NewLog[int]()
		l.NewCursor()
		l.SetLimit(1)
		assert.Equal(t, 1, l.Append(1))
		assert.Equal(t, 0, l.Append(2))
		l.SetLimit(0)
		assert.Equal(t, 1, l.Append(2))
	})

	t.Run("append without cursors stores nothing", func(t *testing.T) {
		l := NewLog[int]()
		assert.Equal(t, 0, l.Append(1))
		assert.Nil(t, l.tail.Load(), "no chunk is allocated while nobody can read")
	})

	t.Run("append links chunks and never copies the one a cursor sits on", func(t *testing.T) {
		l := NewLog[int]()
		c := l.NewCursor()
		first := c.chunk
		assert.NotNil(t, first)
		assert.Equal(t, chunkCapacity, len(first.data))

		for i := 0; i < 3*chunkCapacity; i++ {
			l.Append(i)
		}

		assert.Same(t, first, c.chunk, "unread cursor still sits on its first chunk")
		assert.Equal(t, chunkCapacity, len(first.data), "first chunk was not reallocated")
		assert.NotNil(t, first.next.Load(), "later items live in linked chunks")
		assert.Equal(t, chunkCapacity, len(first.next.Load().data), "chunks do not grow, so memory is freed in small steps")
		assert.Equal(t, 3*chunkCapacity, c.Len())
		assert.Equal(t, 3*chunkCapacity, len(drain(c)))
	})

	t.Run("chunks behind the slowest cursor are garbage collected", func(t *testing.T) {
		l := NewLog[int]()
		a, b := l.NewCursor(), l.NewCursor()
		first := weak.Make(a.chunk)

		for i := 0; i < 2*chunkCapacity; i++ {
			l.Append(i)
		}

		for i := 0; i < chunkCapacity; i++ {
			a.Dequeue()
			b.Dequeue()
		}
		// both cursors have consumed the first chunk; the next Dequeue steps off it
		a.Dequeue()
		b.Dequeue()

		runtime.GC()
		runtime.GC()
		assert.Nil(t, first.Value(), "nothing references the first chunk once every cursor left it")
	})

	t.Run("a slow cursor keeps its chunk alive", func(t *testing.T) {
		l := NewLog[int]()
		fast, slow := l.NewCursor(), l.NewCursor()
		first := weak.Make(slow.chunk)

		for i := 0; i < 2*chunkCapacity; i++ {
			l.Append(i)
		}
		drain(fast)

		runtime.GC()
		runtime.GC()
		assert.NotNil(t, first.Value(), "slow cursor still has to read it")

		drain(slow)
		runtime.GC()
		runtime.GC()
		assert.Nil(t, first.Value())
	})

	t.Run("closing a cursor drops its chunk reference", func(t *testing.T) {
		l := NewLog[int]()
		a, b := l.NewCursor(), l.NewCursor()
		first := weak.Make(a.chunk)

		for i := 0; i < 2*chunkCapacity; i++ {
			l.Append(i)
		}
		drain(b)
		a.Close()

		runtime.GC()
		runtime.GC()
		assert.Nil(t, first.Value(), "closed cursor must not pin memory")
	})

	t.Run("a cursor opened after the last one closed starts at the head", func(t *testing.T) {
		l := NewLog[int]()
		a := l.NewCursor()
		l.Append(1)
		l.Append(2)
		a.Close()

		b := l.NewCursor()
		assert.Equal(t, 0, b.Len(), "new cursor starts after everything published before it")
		assert.Equal(t, 1, l.Append(3))
		assert.Equal(t, []int{3}, drain(b))
	})

	t.Run("readers crossing chunk boundaries see the same full sequence", func(t *testing.T) {
		const writers, perWriter, readers = 4, 20_000, 6
		const total = writers * perWriter // spans many 1024-slot chunks

		l := NewLog[int]()
		cursors := make([]*Cursor[int], readers)
		for i := range cursors {
			cursors[i] = l.NewCursor()
		}

		seqs := make([][]int, readers)
		var wg sync.WaitGroup
		deadline := time.Now().Add(20 * time.Second)
		for r, c := range cursors {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for len(seqs[r]) < total && time.Now().Before(deadline) {
					if n := c.Len(); n < 0 || n > total {
						t.Errorf("reader %d: Len %d out of range", r, n)
						return
					}
					if v, ok := c.Dequeue(); ok {
						seqs[r] = append(seqs[r], v)
					} else {
						runtime.Gosched()
					}
				}
			}()
		}
		for w := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range perWriter {
					l.Append(w*perWriter + i)
				}
			}()
		}
		wg.Wait()

		seen := make(map[int]bool, total)
		for _, v := range seqs[0] {
			seen[v] = true
		}
		assert.Len(t, seen, total, "every item read exactly once, none skipped at a chunk boundary")
		for r := 1; r < readers; r++ {
			assert.Equal(t, seqs[0], seqs[r], "reader %d saw a different sequence", r)
		}
	})

	t.Run("concurrent append and read", func(t *testing.T) {
		const writers, perWriter, readers = 4, 500, 3
		const total = writers * perWriter

		l := NewLog[int]()
		cursors := make([]*Cursor[int], readers)
		for i := range cursors {
			cursors[i] = l.NewCursor()
		}

		var wg sync.WaitGroup
		got := make([]int, readers)
		// Each writer appends w*perWriter+i in order, so every reader must see
		// each writer's values strictly increasing.
		deadline := time.Now().Add(10 * time.Second)

		for w := 0; w < writers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; i < perWriter; i++ {
					l.Append(w*perWriter + i)
				}
			}(w)
		}

		for r := range cursors {
			wg.Add(1)
			go func(r int) {
				defer wg.Done()
				last := make([]int, writers)
				for w := range last {
					last[w] = -1
				}
				for got[r] < total && time.Now().Before(deadline) {
					if v, ok := cursors[r].Dequeue(); ok {
						w := v / perWriter
						assert.Greater(t, v, last[w], "reader %d: writer %d out of order", r, w)
						last[w] = v
						got[r]++
					} else {
						runtime.Gosched()
					}
				}
			}(r)
		}

		wg.Wait()

		for r := range got {
			assert.Equal(t, total, got[r], "reader %d", r)
		}
	})

	t.Run("appends racing cursor open, close and purge", func(t *testing.T) {
		l := NewLog[int]()
		keep := l.NewCursor()
		const writers, perWriter = 4, 5000

		var wg sync.WaitGroup
		for w := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range perWriter {
					l.Append(w*perWriter + i)
				}
			}()
		}
		done := make(chan struct{})
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					c := l.NewCursor()
					c.Dequeue()
					c.Purge()
					c.Close()
				}
			}
		}()
		wg.Wait()
		close(done)

		assert.Len(t, drain(keep), writers*perWriter, "a cursor open throughout reads every item")
		assert.Equal(t, 0, keep.Len())
	})

	t.Run("values and purge wait for an item still being written", func(t *testing.T) {
		for _, consume := range []bool{false, true} {
			l := NewLog[int]()
			c := l.NewCursor()
			c.consume = consume
			l.Append(1)
			l.head.Add(1) // a writer claimed offset 1 and was descheduled before writing
			l.Append(3)

			got := make(chan []int)
			go func() { got <- c.Values() }()
			select {
			case <-got:
				t.Fatal("Values returned past an unwritten claimed slot")
			case <-time.After(20 * time.Millisecond):
			}

			tail := l.tail.Load()
			tail.data[1] = 2
			tail.ready[1].Store(true)
			assert.Equal(t, []int{1, 2, 3}, <-got, "consume=%v", consume)

			c.Purge()
			assert.Equal(t, 0, c.Len())
			if consume {
				assert.Equal(t, []int{0, 0, 0}, tail.data[:3], "consumer purge clears every skipped slot")
			}
			l.Append(4)
			v, ok := c.Dequeue()
			assert.True(t, ok)
			assert.Equal(t, 4, v)
		}
	})
}
