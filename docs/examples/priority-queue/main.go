package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	// The worker is not started, so jobs wait in the queue.
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println("running", j.Data())
	}, 1, varmq.WithAutoRun(false))

	// A priority queue that holds at most 2 waiting jobs.
	queue := worker.BindPriorityQueue(varmq.WithQueueCapacity(2))

	// Add takes a priority after the data. A lower number runs first.
	low, ok := queue.Add("low", 10)
	fmt.Println("add low (10):", ok)
	urgent, ok := queue.Add("urgent", 1)
	fmt.Println("add urgent (1):", ok)

	// The queue is full, so Add refuses the job and returns a nil handle.
	medium, ok := queue.Add("medium", 5)
	fmt.Println("add medium (5):", ok, "- handle is nil:", medium == nil)
	fmt.Println("Len:", queue.Len(), "- IsFull:", queue.IsFull())

	// Purge drops every waiting job and closes its handle.
	queue.Purge()
	fmt.Println("after Purge, Len:", queue.Len(), "- low:", low.Status(), "- urgent:", urgent.Status())

	// There is room again. Add "low" first and "urgent" second.
	queue.Add("low", 10)
	queue.Add("urgent", 1)

	// Once the worker starts, "urgent" runs first although it was added last.
	worker.Start()
	worker.WaitUntilIdle()

	// Close stops the queue taking jobs. It has room, so the refusal here is
	// not about capacity.
	queue.Close()
	_, ok = queue.Add("late", 1)
	fmt.Println("after Close, add late:", ok, "- IsFull:", queue.IsFull())
}
