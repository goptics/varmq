package main

import (
	"fmt"
	"time"

	"github.com/fahimfaisaal/gocq/v2"
	"github.com/fahimfaisaal/gocq/v2/providers"
)

// func main() {
// 	start := time.Now()
// 	defer func() {
// 		fmt.Println("Time taken:", time.Since(start))
// 	}()

// 	q := gocq.NewQueue(10, func(url string) (string, error) {
// 		fmt.Println("Scraping:", url)
// 		time.Sleep(1 * time.Second)
// 		splitUrl := strings.Split(url, "/")
// 		n := splitUrl[3]

// 		// simulate a panic for every 15th url
// 		if num, err := strconv.Atoi(n); err == nil && num%15 == 0 {
// 			panic(fmt.Sprintf("I am panicking for url: %s", url))
// 		}

// 		// simulate an error for every 10th url
// 		if num, err := strconv.Atoi(n); err == nil && num%10 == 0 {
// 			return "", fmt.Errorf("error scraping %s", url)
// 		}

// 		return fmt.Sprintf("Scraped content of %s", url), nil
// 	}).Pause()
// 	defer q.Close()

// 	links := make([]gocq.Item[string], 0)
// 	for i := range 100 {
// 		links = append(links, gocq.Item[string]{Value: fmt.Sprintf("https://example.com/%d", i+1), ID: strconv.Itoa(i + 1)})
// 	}

// 	job := q.AddAll(links)
// 	fmt.Println("Pending jobs:", q.PendingCount())
// 	q.Resume()

// 	results, _ := job.Results()

// 	// this calls will return error, cause result channel has already been consumed
// 	// in order to keep the channel response consistent
// 	job.Drain()
// 	job.Results()
// 	job.Drain()

// 	for result := range results {
// 		if result.Err != nil {
// 			fmt.Printf("Error: %v\n", result.Err)
// 			continue
// 		}
// 		fmt.Println(result.Data)
// 	}

// }

func main() {
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()

	redisQueue := providers.NewRedisQueue("scraping_queue", "redis://localhost:6375")

	pq := gocq.NewPersistentQueue[string, string](200, redisQueue)
	defer pq.Close()

	var w gocq.Worker[string, string]

	w = func(url string) (string, error) {
		fmt.Println("Scraping:", url)
		time.Sleep(1 * time.Second)
		return fmt.Sprintf("Scraped content of %s", url), nil
	}
	err := pq.SetWorker(w)

	if err != nil {
		panic(err)
	}

	// for i := range 10000 {
	// 	pq.Add(fmt.Sprintf("https://example.com/%s", strconv.Itoa(i)), strconv.Itoa(i))
	// }

	// fmt.Println("added jobs")
	fmt.Println("pending jobs:", pq.PendingCount())
}
