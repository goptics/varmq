package gocq

import (
	"runtime"
	"testing"
)

func BenchmarkVoidQueue_Operations(b *testing.B) {
	cpus := uint32(runtime.NumCPU())

	b.Run("Add", func(b *testing.B) {
		q := NewVoidQueue(cpus, func(data int) error {
			Double(data)
			return nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			q.Add(j)
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewVoidQueue(cpus, func(data int) error {
			Double(data)
			return nil
		})
		defer q.WaitAndClose()

		data := make([]int, b.N)
		for i := range data {
			data[i] = i
		}

		b.ResetTimer()
		q.AddAll(data)
	})
}

func BenchmarkVoidPriorityQueue_Operations(b *testing.B) {
	cpus := uint32(runtime.NumCPU())

	b.Run("Add", func(b *testing.B) {
		q := NewVoidPriorityQueue(cpus, func(data int) error {
			Double(data)
			return nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.Add(i, i%10)
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewVoidPriorityQueue(cpus, func(data int) error {
			Double(data)
			return nil
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
