package gocq

import (
	"fmt"
	"runtime"
	"testing"
)

// Double multiplies the input by 2.
func Double(n int) int {
	return n * 2
}

// BenchmarkQueue_Operations benchmarks the operations of Queue.
func BenchmarkQueue_Operations(b *testing.B) {
	cpus := uint(runtime.NumCPU())

	b.Run("Add", func(b *testing.B) {
		q := NewQueue(cpus, func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.Close()

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
		defer q.Close()

		data := make([]int, b.N)
		for i := range data {
			data[i] = i
		}
		fmt.Println("Done data")

		b.ResetTimer()
		out, _ := q.AddAll(data)
		for range out {
			// drain the channel
		}
		fmt.Println("Done out")
	})
}

// BenchmarkPriorityQueue_Operations benchmarks the operations of PriorityQueue.
func BenchmarkPriorityQueue_Operations(b *testing.B) {
	cpus := uint(runtime.NumCPU())

	b.Run("Add", func(b *testing.B) {
		q := NewPriorityQueue(cpus, func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.Add(i, i%10)
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewPriorityQueue(cpus, func(data int) (int, error) {
			return Double(data), nil
		})
		defer q.Close()

		data := make([]PQItem[int], b.N)
		for i := range data {
			data[i] = PQItem[int]{Value: i, Priority: i % 10}
		}
		fmt.Println("Done data")

		b.ResetTimer()
		out, _ := q.AddAll(data)

		for range out {
			// drain the channel
		}
		fmt.Println("Done out")
	})
}
