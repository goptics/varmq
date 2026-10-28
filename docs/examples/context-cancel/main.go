package main

import (
	"context"
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	worker := varmq.NewWorker(
		func(j varmq.Job[int]) {
			time.Sleep(200 * time.Millisecond)
			fmt.Println("job", j.Data(), "done")
		},
		varmq.WithContext(ctx),
	)
	queue := worker.BindQueue()

	queue.Add(1)
	time.Sleep(10 * time.Millisecond) // job 1 is running
	queue.Add(2)
	queue.Add(3)

	cancel()
	time.Sleep(10 * time.Millisecond) // the worker notices on its own goroutine
	fmt.Println("after cancel:", worker.Status(), "- running:", worker.NumProcessing())

	worker.WaitUntilStopped()
	fmt.Println("stopped:", worker.Status(), "- pending:", worker.NumPending(), "- queue Len:", queue.Len())

	_, ok := queue.Add(4)
	fmt.Println("Add after cancel:", ok, "- queue Len:", queue.Len())
}
