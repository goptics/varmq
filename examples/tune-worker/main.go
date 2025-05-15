package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	initialConcurrency := 10
	tuneType := "expand"

	w := varmq.NewVoidWorker(func(data int) {
		fmt.Printf("Processing: %d\n", data)
		randomDuration := time.Duration(rand.Intn(1001)+500) * time.Millisecond // Random between 500-1500ms
		time.Sleep(randomDuration)
	}, initialConcurrency)

	ticker := time.NewTicker(1 * time.Second)
	q := w.BindQueue()
	defer ticker.Stop()
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()
	defer q.WaitAndClose()
	defer fmt.Println("Added jobs")

	// use tuner to tune concurrency
	go func() {
		for range ticker.C {
			if tuneType == "expand" {
				initialConcurrency += 10
			} else {
				initialConcurrency -= 10
			}

			if initialConcurrency >= 100 {
				tuneType = "shrink"
			}

			if initialConcurrency <= 10 {
				tuneType = "expand"
			}

			fmt.Println("Tuning concurrency to", initialConcurrency, "type", tuneType)
			w.TuneConcurrency(initialConcurrency)
			fmt.Println("Current concurrency", initialConcurrency, "type", tuneType)
		}
	}()

	for i := range 1000 {
		q.Add(i)
	}
}
