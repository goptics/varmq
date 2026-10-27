package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		time.Sleep(100 * time.Millisecond)
		fmt.Println("job", j.Data(), "done")
	}, varmq.WithAutoRun(false))
	queue := worker.BindQueue()

	for i := 1; i <= 3; i++ {
		queue.Add(i)
	}
	fmt.Println("created:", worker.Status())

	worker.Start()
	time.Sleep(10 * time.Millisecond) // job 1 is running
	fmt.Println("started:", worker.Status())

	worker.Pause()
	fmt.Println("Pause:", worker.Status())

	worker.WaitUntilPaused()
	fmt.Println("paused:", worker.Status(), "- pending:", worker.NumPending())

	worker.Resume()
	worker.WaitUntilIdle()
	fmt.Println("drained:", worker.Status())
}
