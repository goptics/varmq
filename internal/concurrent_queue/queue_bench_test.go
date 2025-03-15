package concurrent_queue

import (
	"testing"

	"github.com/fahimfaisaal/gocq/internal/common"
	"github.com/fahimfaisaal/gocq/types"
)

// BenchmarkQueue_Operations benchmarks the operations of Queue.
func BenchmarkQueue_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		q := NewQueue(common.Cpus(), func(data int) (int, error) {
			return common.Double(data), nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			q.Add(j).WaitForResult()
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewQueue(common.Cpus(), func(data int) (int, error) {
			return common.Double(data), nil
		})
		defer q.WaitAndClose()

		data := make([]int, common.AddAllSampleSize)
		for i := range data {
			data[i] = i
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for range q.AddAll(data).Results() {
				// drain the channel
			}
		}
	})
}

// BenchmarkQueue_ParallelOperations benchmarks parallel operations of Queue.
func BenchmarkQueue_ParallelOperations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		q := NewQueue(common.Cpus(), func(data int) (int, error) {
			return common.Double(data), nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				q.Add(1)
			}
		})
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewQueue(common.Cpus(), func(data int) (int, error) {
			return common.Double(data), nil
		})
		defer q.WaitAndClose()

		data := make([]int, common.AddAllSampleSize)
		for i := range data {
			data[i] = i
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				for range q.AddAll(data).Results() {
					// drain the channel
				}
			}
		})
	})
}

// BenchmarkPriorityQueue_Operations benchmarks the operations of PriorityQueue.
func BenchmarkPriorityQueue_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		q := NewPriorityQueue(common.Cpus(), func(data int) (int, error) {
			return common.Double(data), nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.Add(i, i%10).WaitForResult()
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewPriorityQueue(common.Cpus(), func(data int) (int, error) {
			return common.Double(data), nil
		})
		defer q.WaitAndClose()

		data := make([]types.PQItem[int], common.AddAllSampleSize)
		for i := range data {
			data[i] = types.PQItem[int]{Value: i, Priority: i % 10}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for range q.AddAll(data).Results() {
				// drain the channel
			}
		}
	})
}

// BenchmarkPriorityQueue_ParallelOperations benchmarks parallel operations of PriorityQueue.
func BenchmarkPriorityQueue_ParallelOperations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		q := NewPriorityQueue(common.Cpus(), func(data int) (int, error) {
			return common.Double(data), nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				q.Add(1, 0).WaitForResult()
			}
		})
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewPriorityQueue(common.Cpus(), func(data int) (int, error) {
			return common.Double(data), nil
		})
		defer q.WaitAndClose()

		data := make([]types.PQItem[int], common.AddAllSampleSize)
		for i := range data {
			data[i] = types.PQItem[int]{Value: i, Priority: i % 10}
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				for range q.AddAll(data).Results() {
					// drain the channel
				}
			}
		})
	})
}
