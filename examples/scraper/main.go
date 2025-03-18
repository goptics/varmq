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
	})
	defer q.WaitAndClose()
	links := make([]string, 0)

	for i := range 100 {
		links = append(links, fmt.Sprintf("https://example.com/%d", i+1))
	}

	for result := range q.AddAll(links).Results() {
		if result.Err != nil {
			fmt.Printf("Error: %v\n", result.Err)
			continue
		}
		fmt.Println(result.Data)
	}
}
