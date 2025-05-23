package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	w := varmq.NewWorker(func(data int) {
		time.Sleep(1 * time.Second)
	},
		200, // the initial concurrency
		varmq.WithIdleWorkerExpiryDuration(2*time.Second), // the idle worker expiry duration
		varmq.WithMinIdleWorkerRatio(15),                  // the minimum idle worker ratio which will be exist in the pool based on the concurrency
	)

	ticker := time.NewTicker(1 * time.Second)
	q := w.BindQueue()
	defer ticker.Stop()

	// use tuner to tune concurrency
	go func() {
		for range ticker.C {
			fmt.Println("Total Goroutines:", runtime.NumGoroutine(), "Idle Workers:", w.NumIdleWorkers())
		}
	}()

retry:
	time.Sleep(10 * time.Second)
	for i := range 1000 {
		q.Add(i)
	}
	fmt.Println("Added jobs")
	q.WaitUntilFinished()

	goto retry
}
