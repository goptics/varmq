package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/goptics/varmq"
)

// Pressing Ctrl+C while this program is running will cause the program to terminate gracefully.
// Tasks being processed will continue until they finish, but queued tasks won't process.
func main() {
	// Create a context that will be cancelled when the user presses Ctrl+C (process receives termination signal).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		<-ctx.Done()
		fmt.Println("Ctrl+C pressed, stopping the worker...")
	}()

	w := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Printf("Task #%d started\n", j.Data())
		time.Sleep(3 * time.Second)
		fmt.Printf("Task #%d finished\n", j.Data())
	}, varmq.WithContext(ctx), 10)
	q := w.BindQueue()

	// Send several long running tasks
	for i := range 100 {
		q.Add(i)
	}

	w.WaitUntilFinished()
	fmt.Println("Worker stopped gracefully")
}
