package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	// Concurrency 1 takes one job at a time, so jobs print in the order they are taken.
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println(j.Data())
	}, 1, varmq.WithAutoRun(false))

	alerts := worker.BindQueue(varmq.WithQueuePriority(1))
	reports := worker.BindQueue(varmq.WithQueuePriority(2))
	emails := worker.BindQueue() // no priority: it ranks last

	// Fill the lowest-ranked queue first.
	emails.Add("email 1")
	emails.Add("email 2")
	reports.Add("report 1")
	alerts.Add("alert 1")
	alerts.Add("alert 2")

	worker.Start()
	worker.WaitUntilIdle()
}
