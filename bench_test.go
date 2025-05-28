package varmq

import (
	"testing"
)

func resultTask(j Job[int]) (int, error) {
	return j.Data() * 2, nil
}

func task(j Job[int]) {
	// do nothing
	_ = j.Data() * 2
}

// BenchmarkQueue_Operations benchmarks the operations of Queue.
func BenchmarkQueue_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			job, _ := q.Add(j)
			job.Wait()
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.AddAll(data).Wait()
		}
	})
}

// BenchmarkQueue_ParallelOperations benchmarks parallel operations of Queue.
func BenchmarkQueue_ParallelOperations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if job, ok := q.Add(1); ok {
					job.Wait()
				}
			}
		})
	})

	b.Run("AddAll", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i}
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				q.AddAll(data).Wait()
			}
		})
	})
}

// BenchmarkPriorityQueue_Operations benchmarks the operations of PriorityQueue.
func BenchmarkPriorityQueue_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer q.WaitAndClose()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if job, ok := q.Add(i, i%10); ok {
				job.Wait()
			}
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer q.WaitAndClose()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i, Priority: i % 10}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.AddAll(data).Wait()
		}
	})
}

// BenchmarkPriorityQueue_ParallelOperations benchmarks parallel operations of PriorityQueue.
func BenchmarkPriorityQueue_ParallelOperations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer q.WaitAndClose()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if job, ok := q.Add(1, 0); ok {
					job.Wait()
				}
			}
		})
	})

	b.Run("AddAll", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer q.WaitAndClose()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i, Priority: i % 10}
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				q.AddAll(data).Wait()
			}
		})
	})
}

// BenchmarkResultWorker_Operations benchmarks operations with a VoidWorker.
func BenchmarkResultWorker_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		// Create a void worker (no return value)
		worker := NewResultWorker(resultTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		b.ResetTimer()
		for j := 0; j < b.N; j++ {
			if job, ok := q.Add(j); ok {
				job.Result()
			}
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		// Create a void worker (no return value)
		worker := NewResultWorker(resultTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.AddAll(data).Wait()
		}
	})
}

// BenchmarkResultWorker_ParallelOperations benchmarks parallel operations with a VoidWorker.
func BenchmarkResultWorker_ParallelOperations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		// Create a void worker (no return value)
		worker := NewResultWorker(resultTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if job, ok := q.Add(1); ok {
					job.Result()
				}
			}
		})
	})

	b.Run("AddAll", func(b *testing.B) {
		// Create a void worker (no return value)
		worker := NewResultWorker(resultTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i}
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				q.AddAll(data).Wait()
			}
		})
	})
}
