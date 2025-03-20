package main

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/fahimfaisaal/gocq/v2"
	"github.com/fahimfaisaal/gocq/v2/providers"
)

func generateJobID() string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func main() {
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()

	redisQueue := providers.NewRedisQueue("scraping_queue", "redis://localhost:6375")

	pq := gocq.NewPersistentQueue[[]string, string](1, redisQueue)
	defer pq.Close()

	for i := range 1000 {
		id := generateJobID()
		data := []string{fmt.Sprintf("https://example.com/%s", strconv.Itoa(i)), id}
		pq.Add(data, id)
	}

	fmt.Println("added jobs")
	fmt.Println("pending jobs:", pq.PendingCount())
}
