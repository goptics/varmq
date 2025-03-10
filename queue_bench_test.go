package gocq

import (
	"runtime"
	"testing"
)

// TwoTimes multiplies the input by 2.
func TwoTimes(n int) int {
	return n * 2
}

// BenchmarkQueue_Operations benchmarks the operations of Queue.
func BenchmarkQueue_Operations(b *testing.B) {
	cpus := uint(runtime.NumCPU())

	b.Run("Add", func(b *testing.B) {
		q := NewQueue(cpus, func(data int) int {
			return TwoTimes(data)
		})
		defer q.Close()

		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			<-q.Add(j)
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewQueue(cpus, func(data int) int {
			return TwoTimes(data)
		})
		defer q.Close()

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
	cpus := uint(runtime.NumCPU())

	b.Run("Add", func(b *testing.B) {
		q := NewPriorityQueue(cpus, func(data int) int {
			return TwoTimes(data)
		})
		defer q.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			out := q.Add(i, i%10)
			<-out
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewPriorityQueue(cpus, func(data int) int {
			return TwoTimes(data)
		})
		defer q.Close()

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
