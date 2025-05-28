package varmq

import (
	"errors"
	"testing"
)

func resultTask(j Job[int]) (int, error) {
	return j.Data() * 2, nil
}

func task(j Job[int]) {
	// do nothing
	_ = j.Data() * 2
}

func errTask(j Job[int]) error {
	// Simple task that returns error for odd numbers
	if j.Data()%2 != 0 {
		return errors.New("odd number error")
	}
	return nil
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
		for j := range b.N {
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
		for b.Loop() {
			q.AddAll(data).Wait()
		}
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
		for i := range b.N {
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
		for b.Loop() {
			q.AddAll(data).Wait()
		}
	})
}

// BenchmarkErrWorker_Operations benchmarks operations with an ErrWorker.
func BenchmarkErrWorker_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		// Create an error worker
		worker := NewErrWorker(errTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		b.ResetTimer()
		for j := range b.N {
			if job, ok := q.Add(j); ok {
				job.Err()
			}
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		// Create an error worker
		worker := NewErrWorker(errTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i}
		}

		b.ResetTimer()
		for b.Loop() {
			q.AddAll(data).Wait()
		}
	})
}

// BenchmarkErrPriorityQueue_Operations benchmarks the operations of ErrPriorityQueue.
func BenchmarkErrPriorityQueue_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		// Create an error worker
		worker := NewErrWorker(errTask)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer q.WaitAndClose()

		b.ResetTimer()
		for i := range b.N {
			if job, ok := q.Add(i, i%10); ok {
				job.Err()
			}
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		// Create an error worker
		worker := NewErrWorker(errTask)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer q.WaitAndClose()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i, Priority: i % 10}
		}

		b.ResetTimer()
		for b.Loop() {
			q.AddAll(data).Wait()
		}
	})
}

// BenchmarkResultWorker_Operations benchmarks operations with a ResultWorker.
func BenchmarkResultWorker_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		// Create a result worker
		worker := NewResultWorker(resultTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		b.ResetTimer()
		for j := range b.N {
			if job, ok := q.Add(j); ok {
				job.Result()
			}
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		// Create a result worker
		worker := NewResultWorker(resultTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer q.WaitAndClose()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i}
		}

		b.ResetTimer()
		for b.Loop() {
			q.AddAll(data).Wait()
		}
	})
}

// BenchmarkResultPriorityQueue_Operations benchmarks the operations of ResultPriorityQueue.
func BenchmarkResultPriorityQueue_Operations(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		// Create a result worker
		worker := NewResultWorker(resultTask)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer q.WaitAndClose()

		b.ResetTimer()
		for i := range b.N {
			if job, ok := q.Add(i, i%10); ok {
				job.Result()
			}
		}
	})

	b.Run("AddAll", func(b *testing.B) {
		// Create a result worker
		worker := NewResultWorker(resultTask)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer q.WaitAndClose()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i, Priority: i % 10}
		}

		b.ResetTimer()
		for b.Loop() {
			q.AddAll(data).Wait()
		}
	})
}
