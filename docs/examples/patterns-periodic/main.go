package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println("run:", j.Data())
		time.Sleep(250 * time.Millisecond) // slower than the ticker
	})
	queue := worker.BindQueue()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for i := 1; i <= 6; i++ {
		<-ticker.C
		// A job ID keeps one "sync" in the queue: Add returns false while another one waits.
		if _, ok := queue.Add("sync", varmq.WithJobId("sync")); !ok {
			fmt.Println("tick", i, "skipped: sync already waits")
		}
	}
	worker.WaitUntilIdle()
}
