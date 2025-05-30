package main

import (
	"fmt"
	"time"

	"github.com/goptics/redisq" // Redis adapter (just one of many possible adapters)
	"github.com/goptics/varmq"
)

func main() {
	rdb := redisq.New("redis://localhost:6375")
	defer rdb.Close()

	pq := rdb.NewQueue("scraping_queue")
	defer pq.Close()

	// bind with persistent queue
	w := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Printf("Processing: %d\n", j.Data())
		time.Sleep(1 * time.Second)
		fmt.Println("Processed: ", j.Data())
	})

	// Using redisq adapter (you can use any adapter that implements IPersistentQueue)
	q := w.WithPersistentQueue(pq)
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start).Microseconds(), "ms")
	}()
	defer w.WaitUntilFinished()
	defer func() {
		fmt.Println("Pending jobs:", q.NumPending())
	}()

	// terminate the program to see the persistent pending jobs in the queue
	// before that comment out the following lines to see the results
	for i := range 10 {
		q.Add(i)
	}
}
