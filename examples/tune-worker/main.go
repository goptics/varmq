package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	initialConcurrency := 100

	w := varmq.NewVoidWorker(func(data int) {
		// fmt.Printf("Processing: %d\n", data)
		randomDuration := time.Duration(rand.Intn(1001)+500) * time.Millisecond // Random between 500-1500ms
		time.Sleep(randomDuration)
	}, initialConcurrency)

	ticker := time.NewTicker(1 * time.Second)
	q := w.BindQueue()
	defer ticker.Stop()
	defer time.Sleep(50 * time.Second)
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()
	defer q.WaitUntilFinished()
	defer fmt.Println("Added jobs")

	// use tuner to tune concurrency
	go func() {
		for range ticker.C {
			fmt.Println("Total Goroutines:", runtime.NumGoroutine())
		}
	}()

	for i := range 1000 {
		q.Add(i)
	}

}
