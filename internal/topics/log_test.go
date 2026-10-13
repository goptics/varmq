package topics

import (
	"runtime"
	"sync"
	"testing"
	"time"

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

// appendUntilTrim appends v until the log's base moves, failing after max appends.
func appendUntilTrim[T any](t *testing.T, l *Log[T], v T, max int) {
	t.Helper()
	start := l.base
	for i := 0; i < max; i++ {
		l.Append(v)
		if l.base != start {
			return
		}
	}
	t.Fatalf("log did not trim after %d appends", max)
}

func TestLog(t *testing.T) {
	t.Run("append without cursors stores nothing", func(t *testing.T) {
		l := NewLog[int]()

		assert.Equal(t, 0, l.Append(1))
		assert.Empty(t, l.items)

		c := l.NewCursor()
		assert.Equal(t, 0, c.Len())
	})

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

	t.Run("trim drops items below the slowest open cursor", func(t *testing.T) {
		l := NewLog[int]()
		fast, slow := l.NewCursor(), l.NewCursor()
		for i := 0; i < 8; i++ {
			l.Append(i)
		}
		drain(fast)
		for i := 0; i < 5; i++ {
			slow.Dequeue()
		}

		appendUntilTrim(t, l, 100, 64)

		assert.Equal(t, uint64(5), l.base)
		v, ok := slow.Dequeue()
		assert.True(t, ok)
		assert.Equal(t, 5, v)
	})

	t.Run("trim waits until half the log is read", func(t *testing.T) {
		l := NewLog[int]()
		fast, slow := l.NewCursor(), l.NewCursor()
		for i := 0; i < 8; i++ {
			l.Append(i)
		}
		drain(fast)
		for i := 0; i < 3; i++ {
			slow.Dequeue()
		}

		for i := 8; i < 16; i++ {
			l.Append(i)
		}
		assert.Equal(t, uint64(0), l.base, "3 of 16 read is under half, so no trim")

		// fast must also read the second batch, or it holds the trim at 8.
		drain(fast)
		for i := 0; i < 10; i++ {
			slow.Dequeue()
		}

		appendUntilTrim(t, l, 100, 64)

		assert.Equal(t, uint64(13), l.base)
	})

	t.Run("closed cursor stops pinning memory", func(t *testing.T) {
		l := NewLog[int]()
		stuck, reader := l.NewCursor(), l.NewCursor()
		for i := 0; i < 8; i++ {
			l.Append(i)
		}
		drain(reader)
		stuck.Close()

		appendUntilTrim(t, l, 100, 64)

		assert.Equal(t, uint64(8), l.base)
	})

	t.Run("trim zeroes the freed tail", func(t *testing.T) {
		l := NewLog[*int]()
		fast, slow := l.NewCursor(), l.NewCursor()
		for i := 0; i < 8; i++ {
			v := i
			l.Append(&v)
		}
		drain(fast)
		for i := 0; i < 6; i++ {
			slow.Dequeue()
		}

		appendUntilTrim(t, l, new(int), 64)

		tail := l.items[len(l.items):cap(l.items)]
		assert.NotEmpty(t, tail)
		for i, p := range tail {
			assert.Nil(t, p, "slot %d past len must be zeroed", i)
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
		deadline := time.Now().Add(10 * time.Second)

		for w := 0; w < writers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < perWriter; i++ {
					l.Append(i)
				}
			}()
		}

		for r := range cursors {
			wg.Add(1)
			go func(r int) {
				defer wg.Done()
				for got[r] < total && time.Now().Before(deadline) {
					if _, ok := cursors[r].Dequeue(); ok {
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
