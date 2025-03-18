package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fahimfaisaal/gocq/v2"
)

func main() {
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()

	q := gocq.NewQueue(10, func(url string) (string, error) {
		fmt.Println("Scraping:", url)
		time.Sleep(1 * time.Second)
		splitUrl := strings.Split(url, "/")
		// simulate an error for every 10th url
		if num, err := strconv.Atoi(splitUrl[3]); err == nil && num%10 == 0 {
			return "", fmt.Errorf("error scraping %s", url)
		}

		return fmt.Sprintf("Scraped content of %s", url), nil
	}).Pause()
	defer q.WaitAndClose()
	links := make([]gocq.Item[string], 0)

	for i := range 20 {
		links = append(links, gocq.Item[string]{Value: fmt.Sprintf("https://example.com/%d", i), ID: strconv.Itoa(i)})
	}

	q.AddAll(links)
	q.Resume()
}
