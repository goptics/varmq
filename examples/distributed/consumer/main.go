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

	pq := gocq.NewPersistentQueue[[]string, string](2, redisQueue)

	err := pq.SetWorker(func(data []string) (string, error) {
		url, id := data[0], data[1]
		fmt.Printf("Scraping url: %s, id: %s\n", url, id)

		time.Sleep(1 * time.Second)
		return fmt.Sprintf("Scraped content of %s id:", url), nil
	})

	if err != nil {
		panic(err)
	}

	fmt.Println("pending jobs:", pq.PendingCount())
	fmt.Println("listening...")
}
