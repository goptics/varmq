package varmq

import (
	"errors"
	"fmt"
	"testing"
)

func resultTask(j Job[int]) (int, error) {
	return j.Data() * 2, nil
}

func task(j Job[int]) {
	_ = j.Data() * 2
}

func errTask(j Job[int]) error {
	// Simple task that returns error for odd numbers
	if j.Data()%2 != 0 {
		return errors.New("odd number error")
	}
	return nil
}

// BenchmarkPublish benchmarks topic fan-out: one op is one Publish, and the
// timer runs until every subscribed worker has processed every payload.
func BenchmarkPublish(b *testing.B) {
	for _, subscribers := range []int{1, 10} {
		b.Run(fmt.Sprintf("Subscribers=%d", subscribers), func(b *testing.B) {
			topic := NewTopic[int]()
			workers := make([]IWorkerBinder[int], subscribers)
			for i := range workers {
				workers[i] = NewWorker(task)
				workers[i].WithTopic(topic)
				defer workers[i].StopAndWait()
			}

			b.ResetTimer()
			for i := range b.N {
				topic.Publish(i)
			}

			for _, w := range workers {
				w.Wait()
			}
		})
	}
}

// BenchmarkAdd benchmarks the Add operation across different queue types.
func BenchmarkAdd(b *testing.B) {
	b.Run("Queue", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer worker.StopAndWait()

		b.ResetTimer()
		for j := range b.N {
			job, _ := q.Add(j)
			job.Wait()
		}
	})

	b.Run("Priority", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer worker.StopAndWait()

		b.ResetTimer()
		for i := range b.N {
			if job, ok := q.Add(i, i%10); ok {
				job.Wait()
			}
		}
	})

	b.Run("ErrQueue", func(b *testing.B) {
		// Create an error worker
		worker := NewErrWorker(errTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer worker.StopAndWait()

		b.ResetTimer()
		for j := range b.N {
			if job, ok := q.Add(j); ok {
				job.Err()
			}
		}
	})

	b.Run("ErrPriority", func(b *testing.B) {
		// Create an error worker
		worker := NewErrWorker(errTask)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer worker.StopAndWait()

		b.ResetTimer()
		for i := range b.N {
			if job, ok := q.Add(i, i%10); ok {
				job.Err()
			}
		}
	})

	b.Run("ResultQueue", func(b *testing.B) {
		// Create a result worker
		worker := NewResultWorker(resultTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer worker.StopAndWait()

		b.ResetTimer()
		for j := range b.N {
			if job, ok := q.Add(j); ok {
				job.Result()
			}
		}
	})

	b.Run("ResultPriority", func(b *testing.B) {
		// Create a result worker
		worker := NewResultWorker(resultTask)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer worker.StopAndWait()

		b.ResetTimer()
		for i := range b.N {
			if job, ok := q.Add(i, i%10); ok {
				job.Result()
			}
		}
	})
}

// BenchmarkAddAll benchmarks the AddAll operation across different queue types.
func BenchmarkAddAll(b *testing.B) {
	b.Run("Queue", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer worker.StopAndWait()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i}
		}

		b.ResetTimer()
		for b.Loop() {
			q.AddAll(data).Wait()
		}
	})

	b.Run("Priority", func(b *testing.B) {
		// Create a worker with the double function
		worker := NewWorker(task)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer worker.StopAndWait()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i, Priority: i % 10}
		}

		b.ResetTimer()
		for b.Loop() {
			q.AddAll(data).Wait()
		}
	})

	b.Run("ErrQueue", func(b *testing.B) {
		// Create an error worker
		worker := NewErrWorker(errTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer worker.StopAndWait()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i}
		}

		b.ResetTimer()
		for b.Loop() {
			q.AddAll(data).Wait()
		}
	})

	b.Run("ErrPriority", func(b *testing.B) {
		// Create an error worker
		worker := NewErrWorker(errTask)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer worker.StopAndWait()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i, Priority: i % 10}
		}

		b.ResetTimer()
		for b.Loop() {
			q.AddAll(data).Wait()
		}
	})

	b.Run("ResultQueue", func(b *testing.B) {
		// Create a result worker
		worker := NewResultWorker(resultTask)
		// Bind the worker to a standard queue
		q := worker.BindQueue()
		defer worker.StopAndWait()

		data := make([]Item[int], 1000) // Using a constant size of 1000 for testing
		for i := range data {
			data[i] = Item[int]{Data: i}
		}

		b.ResetTimer()
		for b.Loop() {
			q.AddAll(data).Wait()
		}
	})

	b.Run("ResultPriority", func(b *testing.B) {
		// Create a result worker
		worker := NewResultWorker(resultTask)
		// Bind the worker to a priority queue
		q := worker.BindPriorityQueue()
		defer worker.StopAndWait()

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
