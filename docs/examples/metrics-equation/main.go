package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("ran job", j.Data())
	})
	queue := worker.BindQueue()
	m := worker.Metrics()

	show := func(label string) {
		fmt.Printf("%s: submitted %d, pending %d + processing %d + completed %d\n",
			label, m.Submitted(), worker.NumPending(), worker.NumProcessing(), m.Completed())
	}

	queue.Add(1)
	worker.WaitUntilIdle()
	show("idle")

	worker.PauseAndWait()
	queue.Add(2)
	queue.Add(3)
	show("paused")

	queue.Purge()
	show("purged")
}
