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
	rq := redisQueue.NewDistributedQueue("scraping_queue")
	defer rq.Close()
	defer rq.Listen()

	w := gocq.NewVoidWorker(func(data []string) {
		url, id := data[0], data[1]
		fmt.Printf("Scraping url: %s, id: %s\n", url, id)
		time.Sleep(1 * time.Second)
		fmt.Printf("Scraped url: %s, id: %s\n", url, id)
	})

	q := w.BindWithDistributedQueue(rq)

	fmt.Println("pending jobs:", q.PendingCount())
	fmt.Println("listening...")
}
