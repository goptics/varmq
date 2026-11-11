package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	start := time.Now()
	done := make(chan struct{})

	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Printf("%s after %v\n", j.Data(), time.Since(start).Round(100*time.Millisecond))
		if j.Data() == "reminder" {
			close(done)
		}
	})
	queue := worker.BindQueue()

	// The timer holds the job. The queue gets it when the delay is over.
	time.AfterFunc(500*time.Millisecond, func() { queue.Add("reminder") })
	queue.Add("welcome")

	// WaitUntilIdle would return before the timer fires.
	<-done
}
