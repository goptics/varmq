package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	// Not started, so all three jobs wait in the queue together.
	worker := varmq.NewWorker(
		func(j varmq.Job[string]) {
			fmt.Println("running", j.Data())
		},
		varmq.WithAutoRun(false),
	)

	queue := worker.BindPriorityQueue()

	queue.Add("report", 10)
	queue.Add("alert", 1)
	queue.Add("digest", 10)

	worker.Start()
	worker.WaitUntilIdle()
}
