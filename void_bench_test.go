package gocq

import (
	"testing"

	"github.com/fahimfaisaal/gocq/v2/internal/common"
)

// BenchmarkVoidQueue_Operations benchmarks the operations of VoidQueue.
func BenchmarkVoidQueue_Operations(b *testing.B) {
	worker := func(data int) error {
		common.Double(data)
		return nil
	}
	b.Run("Add", func(b *testing.B) {
		q := NewVoidQueue(0, worker)
		defer q.WaitAndClose()

		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			q.Add(j).WaitForResult()
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewVoidQueue(0, worker)
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

// BenchmarkVoidQueue_ParallelOperations benchmarks parallel operations of VoidQueue.
func BenchmarkVoidQueue_ParallelOperations(b *testing.B) {
	worker := func(data int) error {
		common.Double(data)
		return nil
	}

	b.Run("Add", func(b *testing.B) {
		q := NewVoidQueue(0, worker)
		defer q.WaitAndClose()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				q.Add(1).WaitForResult()
			}
		})
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewVoidQueue(0, worker)
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

// BenchmarkVoidPriorityQueue_Operations benchmarks the operations of VoidPriorityQueue.
func BenchmarkVoidPriorityQueue_Operations(b *testing.B) {
	worker := func(data int) error {
		common.Double(data)
		return nil
	}
	b.Run("Add", func(b *testing.B) {
		q := NewVoidPriorityQueue(0, worker)
		defer q.WaitAndClose()

		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			q.Add(j, j%10).WaitForResult()
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewVoidPriorityQueue(0, worker)
		defer q.WaitAndClose()

		data := make([]PQItem[int], common.AddAllSampleSize)
		for i := range data {
			data[i] = PQItem[int]{Value: i, Priority: i % 10}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for range q.AddAll(data).Results() {
				// drain the channel
			}
		}
	})
}

// BenchmarkVoidPriorityQueue_ParallelOperations benchmarks parallel operations of VoidPriorityQueue.
func BenchmarkVoidPriorityQueue_ParallelOperations(b *testing.B) {
	worker := func(data int) error {
		common.Double(data)
		return nil
	}

	b.Run("Add", func(b *testing.B) {
		q := NewVoidPriorityQueue(0, worker)
		defer q.WaitAndClose()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				q.Add(1, 0).WaitForResult()
			}
		})
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewVoidPriorityQueue(0, worker)
		defer q.WaitAndClose()

		data := make([]PQItem[int], common.AddAllSampleSize)
		for i := range data {
			data[i] = PQItem[int]{Value: i, Priority: i % 10}
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
