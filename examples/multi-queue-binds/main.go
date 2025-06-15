package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println("Processing:", j.Data())
		time.Sleep(500 * time.Millisecond) // Simulate work
	}) // change strategy through using varmq.WithStrategy default is varmq.RoundRobin
	defer worker.WaitUntilFinished()

	// Bind to a standard queues
	q1 := worker.BindQueue()
	q2 := worker.BindQueue()
	pq := worker.BindPriorityQueue()

	for i := range 10 {
		q1.Add(fmt.Sprintf("Task queue 1 %d", i))
	}

	for i := range 15 {
		q2.Add(fmt.Sprintf("Task queue 2 %d", i))
	}

	for i := range 10 {
		pq.Add(fmt.Sprintf("Task priority queue %d", i), i%2) // prioritize even tasks
	}
}
