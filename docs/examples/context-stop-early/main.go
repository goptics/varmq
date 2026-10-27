package main

import (
	"context"
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	// The function reads the worker's Context(), so declare the worker first.
	var worker varmq.IWorkerBinder[int]
	worker = varmq.NewWorker(func(j varmq.Job[int]) {
		select {
		case <-time.After(time.Second):
			fmt.Println("job", j.Data(), "done")
		case <-worker.Context().Done():
			fmt.Println("job", j.Data(), "ended early:", worker.Context().Err())
		}
	}, 1, varmq.WithContext(ctx))
	queue := worker.BindQueue()

	queue.Add(1)
	time.Sleep(100 * time.Millisecond)
	cancel()

	worker.WaitUntilStopped()
	fmt.Println("stopped:", worker.Status())
}
