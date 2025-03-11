package gocq

import (
	"runtime"
	"testing"
)

// Double multiplies the input by 2.
func Double(n int) int {
	return n * 2
}

// BenchmarkQueue_Operations benchmarks the operations of Queue.
func BenchmarkQueue_Operations(b *testing.B) {
	cpus := uint32(runtime.NumCPU())

	b.Run("Add", func(b *testing.B) {
		q := NewQueue(cpus, func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			output, _ := q.Add(j)
			<-output
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewQueue(cpus, func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.WaitAndClose()

		data := make([]int, b.N)
		for i := range data {
			data[i] = i
		}

		b.ResetTimer()
		out := q.AddAll(data)
		for range out {
			// drain the channel
		}
	})
}

// BenchmarkPriorityQueue_Operations benchmarks the operations of PriorityQueue.
func BenchmarkPriorityQueue_Operations(b *testing.B) {
	cpus := uint32(runtime.NumCPU())

	b.Run("Add", func(b *testing.B) {
		q := NewPriorityQueue(cpus, func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			output, _ := q.Add(i, i%10)
			<-output // Wait for each operation to complete
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewPriorityQueue(cpus, func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.WaitAndClose()

		data := make([]PQItem[int], b.N)
		for i := range data {
			data[i] = PQItem[int]{Value: i, Priority: i % 10}
		}

		b.ResetTimer()
		out := q.AddAll(data)
		for range out {
			// drain the channel
		}
	})
}
