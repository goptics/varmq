package main

import (
	"fmt"
	"time"

	"github.com/fahimfaisaal/gocmq"
	"github.com/fahimfaisaal/redisq" // Redis adapter (just one of many possible adapters)
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
	w := gocmq.NewWorker(func(data int) (int, error) {
		fmt.Printf("Processing: %d\n", data)
		time.Sleep(3 * time.Second)
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
	}, 2)

	// Using redisq adapter (you can use any adapter that implements IPersistentQueue)
	q := w.WithPersistentQueue(pq)
	defer q.WaitAndClose()
	defer fmt.Println("pending jobs:", q.PendingCount())

	// terminate the program to see the persistent pending jobs in the queue
	// before that comment out the following lines to see the results
	items := make([]gocmq.Item[int], 10)
	for i := range items {
		items[i] = gocmq.Item[int]{Value: i, ID: fmt.Sprintf("%d", i)}
	}

	q.AddAll(items)
}
