package main

import (
	"fmt"
	"time"

	"github.com/fahimfaisaal/gocq/v2"
	"github.com/fahimfaisaal/gocq/v2/providers"
)

func main() {
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()

	redisQueue := providers.NewRedisQueue("scraping_queue", "redis://localhost:6375")
	defer redisQueue.Listen()

	w := gocq.NewVoidWorker(func(data []string) {
		url, id := data[0], data[1]
		fmt.Printf("Scraping url: %s, id: %s\n", url, id)
		time.Sleep(1 * time.Second)
		fmt.Printf("Scraped url: %s, id: %s\n", url, id)
	}, 1)

	q := w.BindWithDistributedQueue(redisQueue)

	fmt.Println("pending jobs:", q.PendingCount())
	fmt.Println("listening...")
}
