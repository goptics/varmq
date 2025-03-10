package gocq

import (
	"runtime"
	"testing"
)

// BenchmarkVoidQueue_Operations benchmarks the operations of VoidQueue.
func BenchmarkVoidQueue_Operations(b *testing.B) {
	cpus := uint(runtime.NumCPU())

	b.Run("Add", func(b *testing.B) {
		q := NewVoidQueue(cpus, func(data int) {
			TwoTimes(data)
		})
		defer q.Close()

		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			q.Add(j)
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewVoidQueue(cpus, func(data int) {

		})
		defer q.Close()

		data := make([]int, b.N)
		for i := range data {
			data[i] = i
		}

		b.ResetTimer()
		q.AddAll(data)
	})
}

// BenchmarkVoidPriorityQueue_Operations benchmarks the operations of VoidPriorityQueue.
func BenchmarkVoidPriorityQueue_Operations(b *testing.B) {
	cpus := uint(runtime.NumCPU())

	b.Run("Add", func(b *testing.B) {
		q := NewVoidPriorityQueue(cpus, func(data int) {
			TwoTimes(data)
		})
		defer q.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.Add(i, i%10)
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewVoidPriorityQueue(cpus, func(data int) {
			TwoTimes(data)
		})
		defer q.Close()

		data := make([]PQItem[int], b.N)
		for i := range data {
			data[i] = PQItem[int]{Value: i, Priority: i % 10}
		}

		b.ResetTimer()
		q.AddAll(data)
	})
}
