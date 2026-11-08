package varmq

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
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

// BenchmarkPublish benchmarks the Publish operation: one op is one Publish to
// a topic with one subscribed worker, and the timer runs until the worker has
// processed every payload.
func BenchmarkPublish(b *testing.B) {
	b.Run("Topic", func(b *testing.B) {
		topic := NewTopic[int]()
		worker := NewWorker(task)
		worker.WithTopic(topic)
		defer worker.StopAndWait()

		b.ResetTimer()
		for i := range b.N {
			topic.Publish(i)
		}

		worker.Wait()
	})
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

// BenchmarkAddWithId reports what a job id costs an in-memory Add: Unique
// stores and runs every job, Repeat is refused because a job with the same id
// is waiting.
func BenchmarkAddWithId(b *testing.B) {
	ids := make([]string, 1024)
	for i := range ids {
		ids[i] = strconv.Itoa(i)
	}

	b.Run("Unique", func(b *testing.B) {
		worker := NewWorker(task)
		q := worker.BindQueue()
		defer worker.StopAndWait()

		b.ResetTimer()
		for j := range b.N {
			job, _ := q.Add(j, WithJobId(ids[j%len(ids)]))
			job.Wait()
		}
	})

	// Parallel adds from every P with no per-job Wait, so producers contend
	// on the id set the way a busy service would.
	b.Run("Parallel", func(b *testing.B) {
		worker := NewWorker(task)
		q := worker.BindQueue()
		defer worker.StopAndWait()

		var n atomic.Uint64

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				q.Add(1, WithJobId(strconv.FormatUint(n.Add(1), 36)))
			}
		})
	})

	b.Run("Repeat", func(b *testing.B) {
		worker := NewWorker(task, WithAutoRun(false))
		q := worker.BindQueue()
		q.Add(0, WithJobId("a"))

		b.ResetTimer()
		for j := range b.N {
			q.Add(j, WithJobId("a"))
		}
	})
}

// BenchmarkContext reports what Worker.Context costs a worker function that
// reads it for every job, from every P at once.
func BenchmarkContext(b *testing.B) {
	worker := NewWorker(task)
	defer worker.StopAndWait()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = worker.Context()
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

// BenchmarkWorkflowPromotion reports, as promote-ns/op, the time from the end
// of task A's function (just before its job closes) to the start of its
// dependent B, in-memory. Each op submits a fresh A -> B workflow and waits
// for B, so it is promotion latency, not throughput.
func BenchmarkWorkflowPromotion(b *testing.B) {
	ctx := context.Background()
	// ponytail: one store per run; a store only sweeps ended workflows by
	// retention, so a long -benchtime holds its b.N records until the run ends.
	store := NewWorkflowStore()

	var endA atomic.Pointer[time.Time] // when A's function returns
	var total atomic.Int64
	done := make(chan struct{}, 1)

	aw := NewErrWorker(func(j Job[int]) error {
		t := time.Now()
		endA.Store(&t)
		return nil
	})
	bw := NewErrWorker(func(j Job[int]) error {
		total.Add(int64(time.Since(*endA.Load())))
		done <- struct{}{}
		return nil
	})
	defer aw.StopAndWait()
	defer bw.StopAndWait()
	aQ, bQ := aw.BindQueue(), bw.BindQueue()

	b.ResetTimer()
	for i := range b.N {
		wf := NewWorkflow(store, WithWorkflowID(fmt.Sprint("bench-", i)))
		a := wf.Add("a", aQ, i)
		wf.Add("b", bQ, i).After(a)
		if err := wf.Submit(ctx); err != nil {
			b.Fatal(err)
		}
		<-done
	}
	b.StopTimer()

	b.ReportMetric(float64(total.Load())/float64(b.N), "promote-ns/op")
}
