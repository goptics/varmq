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
	w := varmq.NewWorker(func(data int) (int, error) {
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

	// Using redisq adapter (you can use any adapter that implements IPersistentQueue)
	q := w.WithPersistentQueue(pq)
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start).Microseconds(), "ms")
	}()
	defer q.WaitUntilFinished()
	defer func() {
		fmt.Println("Pending jobs:", q.NumPending())
	}()

	// terminate the program to see the persistent pending jobs in the queue
	// before that comment out the following lines to see the results
	items := make([]varmq.Item[int], 10)
	for i := range items {
		items[i] = varmq.Item[int]{Value: i, ID: fmt.Sprintf("%d", i)}
	}

	q.AddAll(items)
}
