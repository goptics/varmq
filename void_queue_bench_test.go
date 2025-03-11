package gocq

import (
	"testing"
)

func BenchmarkVoidQueue_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		q := NewVoidQueue(Cpus(), func(data int) error {
			Double(data)
			return nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			for i := range SampleSize {
				err := q.Add(i)
				<-err
			}
		}
	})

	b.Run("Add-Parallel", func(b *testing.B) {
		q := NewVoidQueue(Cpus(), func(data int) error {
			Double(data)
			return nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				err := q.Add(1)
				<-err
			}
		})
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewVoidQueue(Cpus(), func(data int) error {
			Double(data)
			return nil
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

func BenchmarkVoidPriorityQueue_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		q := NewVoidPriorityQueue(Cpus(), func(data int) error {
			Double(data)
			return nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.Add(i, i%10)
		}
	})

	b.Run("Add-Parallel", func(b *testing.B) {
		q := NewVoidPriorityQueue(Cpus(), func(data int) error {
			Double(data)
			return nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				q.Add(1, 0)
			}
		})
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewVoidPriorityQueue(Cpus(), func(data int) error {
			Double(data)
			return nil
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
		q := NewVoidPriorityQueue(Cpus(), func(data int) error {
			Double(data)
			return nil
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
