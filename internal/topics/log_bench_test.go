package topics

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// BenchmarkLog measures Append with N cursors each drained by its own
// goroutine, the way every subscription's event loop reads a topic. One op is
// one Append; the timer runs until every cursor has read every item.
func BenchmarkLog(b *testing.B) {
	for _, subs := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("subscribers=%d", subs), func(b *testing.B) {
			l := NewLog[int]()
			cursors := make([]*Cursor[int], subs)
			for i := range cursors {
				cursors[i] = l.NewCursor()
			}

			var appended atomic.Int64
			var stop atomic.Bool
			var readers sync.WaitGroup
			for _, c := range cursors {
				readers.Add(1)
				go func() {
					defer readers.Done()
					var read int64
					for {
						if _, ok := c.Dequeue(); ok {
							read++
							continue
						}
						if stop.Load() && read == appended.Load() {
							return
						}
						runtime.Gosched()
					}
				}()
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					l.Append(1)
					appended.Add(1)
				}
			})
			stop.Store(true)
			readers.Wait()
		})
	}
}

// BenchmarkLogSequential is one Append followed by one Dequeue on a single
// goroutine: the uncontended cost of the log itself.
func BenchmarkLogSequential(b *testing.B) {
	l := NewLog[int]()
	c := l.NewCursor()
	for b.Loop() {
		l.Append(1)
		c.Dequeue()
	}
}
