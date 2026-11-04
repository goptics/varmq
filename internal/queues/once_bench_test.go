package queues

import (
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
)

// benchIds is shared by the parallel benches. The queue never holds more than
// maxBacklog items, so an id is long gone before the counter comes back round
// to it.
var benchIds = func() []idItem {
	ids := make([]idItem, 1<<20)
	for i := range ids {
		ids[i] = idItem(strconv.Itoa(i))
	}
	return ids
}()

const maxBacklog = 10000

type benchQueue interface {
	Dequeue() (any, bool)
	Len() int
}

// BenchmarkEnqueueOnceParallel adds from every P while one goroutine
// dequeues, as the worker's event loop does. Adds wait while maxBacklog items
// are queued, so the consumer keeps up and every id is new.
func BenchmarkEnqueueOnceParallel(b *testing.B) {
	run := func(b *testing.B, q benchQueue, add func(idItem) bool) {
		var n, refused atomic.Uint64
		var stop atomic.Bool
		done := make(chan struct{})
		go func() {
			defer close(done)
			for !stop.Load() {
				if _, ok := q.Dequeue(); !ok {
					runtime.Gosched()
				}
			}
		}()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				for q.Len() >= maxBacklog {
					runtime.Gosched()
				}
				if !add(benchIds[n.Add(1)&(1<<20-1)]) {
					refused.Add(1)
				}
			}
		})
		b.StopTimer()
		stop.Store(true)
		<-done
		b.ReportMetric(float64(refused.Load())/float64(b.N), "refused/op")
	}

	b.Run("Queue/Enqueue", func(b *testing.B) {
		q := NewQueue[idItem]()
		run(b, q, func(i idItem) bool { return q.Enqueue(i) })
	})
	b.Run("Queue/EnqueueOnce", func(b *testing.B) {
		q := NewQueue[idItem]()
		run(b, q, func(i idItem) bool { return q.EnqueueOnce(i, string(i)) })
	})
	b.Run("Priority/Enqueue", func(b *testing.B) {
		q := NewPriorityQueue[idItem]()
		run(b, q, func(i idItem) bool { return q.Enqueue(i, 0) })
	})
	b.Run("Priority/EnqueueOnce", func(b *testing.B) {
		q := NewPriorityQueue[idItem]()
		run(b, q, func(i idItem) bool { return q.EnqueueOnce(i, 0, string(i)) })
	})
}
