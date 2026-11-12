package main

import (
	"context"
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

// work takes as long as the job's data says, unless ctx ends first.
func work(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func main() {
	worker := varmq.NewErrWorker(func(j varmq.Job[time.Duration]) error {
		// j.Context() also ends when the job is cancelled.
		ctx, cancel := context.WithTimeout(j.Context(), 200*time.Millisecond)
		defer cancel()
		return work(ctx, j.Data())
	}, 2)
	queue := worker.BindQueue()

	fast, _ := queue.Add(50 * time.Millisecond)
	slow, _ := queue.Add(time.Second)

	fmt.Println("fast:", fast.Err())
	fmt.Println("slow:", slow.Err())
}
