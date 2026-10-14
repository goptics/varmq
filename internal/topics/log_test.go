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

	t.Run("append without cursors stores nothing", func(t *testing.T) {
		l := NewLog[int]()
		assert.Equal(t, 0, l.Append(1))
		assert.Nil(t, l.tail, "no chunk is allocated while nobody can read")
	})

	t.Run("append links chunks and never copies the one a cursor sits on", func(t *testing.T) {
		l := NewLog[int]()
		c := l.NewCursor()
		first := c.chunk
		assert.NotNil(t, first)
		assert.Equal(t, initialChunkCapacity, first.Cap())

		for i := 0; i < 3*initialChunkCapacity; i++ {
			l.Append(i)
		}

		assert.Same(t, first, c.chunk, "unread cursor still sits on its first chunk")
		assert.Equal(t, initialChunkCapacity, first.Cap(), "first chunk was not reallocated")
		assert.NotNil(t, first.Next, "later items live in linked chunks")
		assert.Greater(t, first.Next.Cap(), first.Cap(), "chunks grow 1.5x")
		assert.Equal(t, 3*initialChunkCapacity, c.Len())
		assert.Equal(t, 3*initialChunkCapacity, len(drain(c)))
	})

	t.Run("chunks behind the slowest cursor are garbage collected", func(t *testing.T) {
		l := NewLog[int]()
		a, b := l.NewCursor(), l.NewCursor()
		first := weak.Make(a.chunk)

		for i := 0; i < 2*initialChunkCapacity; i++ {
			l.Append(i)
		}

		for i := 0; i < initialChunkCapacity; i++ {
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

		for i := 0; i < 2*initialChunkCapacity; i++ {
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

		for i := 0; i < 2*initialChunkCapacity; i++ {
			l.Append(i)
		}
		drain(b)
		a.Close()

		runtime.GC()
		runtime.GC()
		assert.Nil(t, first.Value(), "closed cursor must not pin memory")
	})

	t.Run("closing the last cursor resets the log", func(t *testing.T) {
		l := NewLog[int]()
		a := l.NewCursor()
		l.Append(1)
		l.Append(2)
		a.Close()

		assert.Nil(t, l.tail)

		b := l.NewCursor()
		assert.Equal(t, 0, b.Len(), "new cursor starts after everything published before it")
		assert.Equal(t, 1, l.Append(3))
		assert.Equal(t, []int{3}, drain(b))
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
}
