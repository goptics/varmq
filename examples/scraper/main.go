package main

import (
	"fmt"
	"time"

	"github.com/fahimfaisaal/gocq/v2"
)

func main() {
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()

	q := gocq.NewWorker(func(data int) (int, error) {
		fmt.Printf("Processing: %d\n", data)
		time.Sleep(1 * time.Second)
		fmt.Printf("Processed: %d\n", data)
		return data * 2, nil
	}, gocq.WithConcurrency(10)).Queue()

	for i := range 1000 {
		q.Add(i)
	}

	fmt.Println("added jobs")
	q.WaitUntilFinished()
}
