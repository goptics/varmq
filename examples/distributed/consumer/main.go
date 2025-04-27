package main

import (
	"fmt"
	"time"

	"github.com/fahimfaisaal/gocmq"
	"github.com/fahimfaisaal/redisq"
)

func main() {
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()

	redisQueue := redisq.New("redis://localhost:6375")
	defer redisQueue.Close()
	rq := redisQueue.NewDistributedQueue("scraping_queue")
	defer rq.Close()
	defer rq.Listen()

	w := gocmq.NewVoidWorker(func(data []string) {
		url, id := data[0], data[1]
		fmt.Printf("Scraping url: %s, id: %s\n", url, id)
		time.Sleep(2 * time.Second)
		fmt.Printf("Scraped url: %s, id: %s\n", url, id)
	}, 5)

	// Using redisq adapter (you can use any adapter that implements IDistributedQueue)
	q := w.WithDistributedQueue(rq)

	fmt.Println("pending jobs:", q.PendingCount())
	fmt.Println("listening...")
}
