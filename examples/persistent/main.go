package main

import (
	"fmt"
	"time"

	"github.com/fahimfaisaal/gocq/v3"
	"github.com/fahimfaisaal/redisq"
)

func main() {
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()

	redisQueue := redisq.New("redis://localhost:6375")
	defer redisQueue.Close()
	pq := redisQueue.NewQueue("scraping_queue")
	defer pq.Close()

	// bind with persistent queue
	w := gocq.NewWorker(func(data int) (int, error) {
		fmt.Printf("Processing: %d\n", data)
		time.Sleep(1 * time.Second)
		r := data * 3

		// error on every 10th job
		if data%10 == 0 {
			return 0, fmt.Errorf("error")
		}

		// panic on every 15th job
		if data%15 == 0 {
			panic("panic")
		}

		return r, nil
	})

	q := w.BindWithPersistentQueue(pq)
	defer q.WaitAndClose()

	items := make([]gocq.Item[int], 1000)
	for i := range items {
		items[i] = gocq.Item[int]{Value: i, ID: fmt.Sprintf("%d", i)}
	}

	fmt.Println("pending jobs:", q.PendingCount())

	// q.AddAll(items)

	// terminate the program to see the persistent pending jobs in the queue
	// comment out the q.AddAll(items) line to see the results
}
