package gocq

import (
	"runtime"
	"testing"
)

const SampleSize = 100

// Double multiplies the input by 2.
func Double(n int) int {
	return n * 2
}

func Cpus() uint32 {
	return uint32(runtime.NumCPU())
}

// BenchmarkQueue_Operations benchmarks the operations of Queue.
func BenchmarkQueue_Operations(b *testing.B) {

	b.Run("Add", func(b *testing.B) {
		q := NewQueue(Cpus(), func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			q.Add(j)
		}
	})

	b.Run("Add-Parallel", func(b *testing.B) {
		q := NewQueue(Cpus(), func(data int) (int, error) {
			return Double(data), nil
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
		q := NewQueue(Cpus(), func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.WaitAndClose()

		data := make([]int, SampleSize)
		for i := range data {
			data[i] = i
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			out := q.AddAll(data)
			for range out {
				// drain the channel
			}
		}
	})

	b.Run("AddAll-Parallel", func(b *testing.B) {
		q := NewQueue(Cpus(), func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.WaitAndClose()

		data := make([]int, SampleSize)
		for i := range data {
			data[i] = i
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				out := q.AddAll(data)
				for range out {
					// drain the channel
				}
			}
		})
	})
}

// BenchmarkPriorityQueue_Operations benchmarks the operations of PriorityQueue.
func BenchmarkPriorityQueue_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		q := NewPriorityQueue(Cpus(), func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.Add(i, i%10)
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewPriorityQueue(Cpus(), func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.WaitAndClose()

		data := make([]PQItem[int], SampleSize)
		for i := range data {
			data[i] = PQItem[int]{Value: i, Priority: i % 10}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			out := q.AddAll(data)
			for range out {
				// drain the channel
			}
		}
	})

	b.Run("AddAll-Parallel", func(b *testing.B) {
		q := NewPriorityQueue(Cpus(), func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.WaitAndClose()

		data := make([]PQItem[int], SampleSize)
		for i := range data {
			data[i] = PQItem[int]{Value: i, Priority: i % 10}
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				out := q.AddAll(data)
				for range out {
					// drain the channel
				}
			}
		})
	})
}
