package main

import (
	"fmt"
	"time"

	"github.com/fahimfaisaal/gocq/v2"
)

func main() {
	q := gocq.NewQueue(4, func(data string) (string, error) {
		fmt.Println("Scraping:", data)
		time.Sleep(1 * time.Second)
		return data, nil
	})
	defer q.WaitAndClose()

	q.Add("https://example.com").Drain()
	q.AddAll([]string{
		"https://example.com/1",
		"https://example.com/2",
		"https://example.com/3",
		"https://example.com/4",
		"https://example.com/5",
		"https://example.com/6",
		"https://example.com/7",
		"https://example.com/8",
		"https://example.com/9",
		"https://example.com/10",
		"https://example.com/11",
		"https://example.com/12",
		"https://example.com/13",
		"https://example.com/14",
		"https://example.com/15",
	}).Results()
}
