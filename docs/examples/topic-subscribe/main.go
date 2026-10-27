package main

import (
	"fmt"
	"slices"
	"sync"

	"github.com/goptics/varmq"
)

type Order struct {
	ID   int
	Item string
}

var (
	mu     sync.Mutex
	emails []int
	stocks []int
)

func sendEmail(j varmq.Job[Order]) {
	mu.Lock()
	defer mu.Unlock()
	emails = append(emails, j.Data().ID)
}

func updateStock(j varmq.Job[Order]) {
	mu.Lock()
	defer mu.Unlock()
	stocks = append(stocks, j.Data().ID)
}

func main() {
	topic := varmq.NewTopic[Order]()

	email := varmq.NewWorker(sendEmail)
	stock := varmq.NewWorker(updateStock, 4)

	email.WithTopic(topic)
	stock.WithTopic(topic)

	for i, item := range []string{"book", "pen", "lamp"} {
		order := Order{ID: i + 1, Item: item}
		topic.Publish(order) // both workers get their own job for this order
	}

	// Publish returns once the order is stored, so wait on each worker.
	email.WaitUntilIdle()
	stock.WaitUntilIdle()

	// The stock worker runs up to 4 jobs at once, so sort before printing.
	slices.Sort(stocks)
	fmt.Println("emails sent for orders", emails)
	fmt.Println("stock updated for orders", stocks)
}
