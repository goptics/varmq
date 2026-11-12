package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	start := time.Now()

	// Five jobs can run at the same time, but only one starts each 200 ms.
	limit := time.NewTicker(200 * time.Millisecond)
	defer limit.Stop()

	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		<-limit.C
		fmt.Printf("call %d at %v\n", j.Data(), time.Since(start).Round(100*time.Millisecond))
	}, 5)
	queue := worker.BindQueue()

	for i := 1; i <= 5; i++ {
		queue.Add(i)
	}
	worker.WaitUntilIdle()
}
