package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/goptics/varmq"
)

// run binds three queues to one worker, fills them, and returns the order in
// which the worker took the jobs under the given strategy.
func run(strategy varmq.Strategy) string {
	var mu sync.Mutex
	var order []string

	// Concurrency 1 and WithAutoRun(false): nothing runs until every queue is
	// filled, and the worker takes one job at a time, so the order is exact.
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		mu.Lock()
		order = append(order, j.Data())
		mu.Unlock()
	}, 1, varmq.WithStrategy(strategy), varmq.WithAutoRun(false))

	alerts := worker.BindQueue(varmq.WithQueuePriority(1))
	reports := worker.BindQueue(varmq.WithQueuePriority(2))
	emails := worker.BindQueue() // no priority: it ranks last

	for range 2 {
		alerts.Add("alerts")
	}
	reports.Add("reports")
	for range 3 {
		emails.Add("emails")
	}

	worker.Start()
	worker.WaitUntilIdle()

	mu.Lock()
	defer mu.Unlock()
	return strings.Join(order, " ")
}

func main() {
	fmt.Println("alerts holds 2 jobs, reports 1, emails 3")
	fmt.Println("Priority:  ", run(varmq.Priority))
	fmt.Println("RoundRobin:", run(varmq.RoundRobin))
	fmt.Println("MaxLen:    ", run(varmq.MaxLen))
	fmt.Println("MinLen:    ", run(varmq.MinLen))
}
