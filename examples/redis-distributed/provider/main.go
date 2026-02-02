package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/goptics/redisq"
	"github.com/goptics/varmq"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

func main() {
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()

	redisQueue := redisq.New("redis://localhost:6375")
	defer redisQueue.Close()
	rq := redisQueue.NewDistributedQueue("scraping_queue")
	pq := varmq.NewDistributedQueue[[]string](rq)
	defer pq.Close()

	for i := range 1000 {
		id, err := gonanoid.New()

		if err != nil {
			panic(err)
		}

		data := []string{fmt.Sprintf("https://example.com/%s", strconv.Itoa(i)), id}
		pq.Add(data, varmq.WithJobId(id))
	}

	fmt.Println("added jobs")
	fmt.Println("pending jobs:", pq.NumPending())

	time.Sleep(5 * time.Second)
	pq.Purge()
	fmt.Println("purged jobs")
}
