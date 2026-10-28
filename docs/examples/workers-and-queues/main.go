package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	// One worker: a pool that runs at most 2 jobs at a time.
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println("Processing", j.Data())
		time.Sleep(500 * time.Millisecond)
	}, 2)

	// Two queues bound to the same worker. Both feed its pool.
	emails := worker.BindQueue()
	reports := worker.BindQueue()

	for i := range 3 {
		emails.Add(fmt.Sprintf("email %d", i))
	}
	for i := range 3 {
		reports.Add(fmt.Sprintf("report %d", i))
	}

	worker.WaitUntilIdle()
	fmt.Println("All jobs done")
}
