package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	initialConcurrency := 10
	tuneType := "expand"

	w := varmq.NewWorker(func(data int) {
		// fmt.Printf("Processing: %d\n", data)
		randomDuration := time.Duration(rand.Intn(1001)+500) * time.Millisecond // Random between 500-1500ms
		time.Sleep(randomDuration)
	}, initialConcurrency)

	q := w.BindQueue()
	ticker := time.NewTicker(1 * time.Second)

	// use tuner to tune concurrency
	go func() {
		for range ticker.C {

			if tuneType == "expand" {
				initialConcurrency += 10
			} else {
				initialConcurrency -= 10
			}

			w.TunePool(initialConcurrency)

			fmt.Printf("Total Goroutines: %d, Idle Workers: %d\nConcurrency: %d, Processing: %d\nPending Jobs: %d Type: %s\n\n", runtime.NumGoroutine(), w.NumIdleWorkers(), initialConcurrency, w.NumProcessing(), q.NumPending(), tuneType)

			if initialConcurrency >= 100 {
				tuneType = "shrink"
			}

			if initialConcurrency <= 10 {
				tuneType = "expand"
			}

		}
	}()
	time.Sleep(3 * time.Second)

retry:
	for i := range 1000 {
		q.Add(i)
	}

	fmt.Println("Added jobs")
	q.WaitUntilFinished()
	time.Sleep(5 * time.Second)
	goto retry
}
