package void_queue

import (
	"testing"

	"github.com/fahimfaisaal/gocq/internal/common"
	"github.com/fahimfaisaal/gocq/types"
)

func BenchmarkVoidQueue_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		q := NewQueue(common.Cpus(), func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for i := range b.N {
			q.Add(i).WaitForError()
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewQueue(common.Cpus(), func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.WaitAndClose()

		data := make([]int, common.AddAllSampleSize)
		for i := range data {
			data[i] = i
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for range q.AddAll(data).Errors() {
				// drain the channel
			}
		}
	})
}

func BenchmarkVoidQueue_ParallelOperations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		q := NewQueue(common.Cpus(), func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				q.Add(1).WaitForError()
			}
		})
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewQueue(common.Cpus(), func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.WaitAndClose()

		data := make([]int, common.AddAllSampleSize)
		for i := range data {
			data[i] = i
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				for range q.AddAll(data).Errors() {
					// drain the channel
				}
			}
		})
	})
}

func BenchmarkVoidPriorityQueue_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		q := NewPriorityQueue(common.Cpus(), func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		for i := range b.N {
			q.Add(i, i%10).WaitForError()
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewPriorityQueue(common.Cpus(), func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.WaitAndClose()

		data := make([]types.PQItem[int], common.AddAllSampleSize)
		for i := range data {
			data[i] = types.PQItem[int]{Value: i, Priority: i % 10}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for range q.AddAll(data).Errors() {
				// drain the channel
			}
		}
	})
}

func BenchmarkVoidPriorityQueue_ParallelOperations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		q := NewPriorityQueue(common.Cpus(), func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.WaitAndClose()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				q.Add(1, 0).WaitForError()
			}
		})
	})

	b.Run("AddAll", func(b *testing.B) {
		q := NewPriorityQueue(common.Cpus(), func(data int) error {
			common.Double(data)
			return nil
		})
		defer q.WaitAndClose()

		data := make([]types.PQItem[int], common.AddAllSampleSize)
		for i := range data {
			data[i] = types.PQItem[int]{Value: i, Priority: i % 10}
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				for range q.AddAll(data).Errors() {
					// drain the channel
				}
			}
		})
	})
}
