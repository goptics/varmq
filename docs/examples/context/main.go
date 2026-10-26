package main

import (
	"context"
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})

	// The function needs the worker to read its context, so declare it first.
	var worker varmq.IWorkerBinder[int]
	worker = varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("job", j.Data(), "started")
		started <- struct{}{}
		<-release // held here, so the program can look at the worker while it runs
		fmt.Println("job", j.Data(), "finished - context cancelled:", worker.Context().Err() != nil)
	}, 1, varmq.WithContext(ctx))
	queue := worker.BindQueue()

	queue.Add(1)
	<-started // job 1 is running; the next two jobs wait behind it
	queue.Add(2)
	queue.Add(3)
	fmt.Println("before cancel:", worker.Status(), "- NumProcessing:", worker.NumProcessing(), "- NumPending:", worker.NumPending())

	cancel()
	// cancel returns at once. The worker notices on its own goroutine, so wait until it has.
	for worker.Status() != "Stopping" {
		time.Sleep(time.Millisecond)
	}
	fmt.Println("after cancel:", worker.Status(), "- NumProcessing:", worker.NumProcessing())

	close(release) // job 1 was never interrupted. It ends when its function returns
	worker.WaitUntilStopped()
	fmt.Println("stopped:", worker.Status(), "- NumPending:", worker.NumPending(), "- queue Len:", queue.Len())

	// The queue still takes jobs. Nothing will run them.
	_, ok := queue.Add(4)
	fmt.Println("Add after cancel:", ok, "- queue Len:", queue.Len())
}
