package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	topic := varmq.NewTopic[int]()

	email := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("email for order", j.Data())
	})

	stock := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("stock for order", j.Data())
	}, 4)

	email.WithTopic(topic)
	stock.WithTopic(topic)

	for i := range 4 {
		topic.Publish(i) // both workers get their own job for this order
	}

	email.WaitUntilIdle()
	stock.WaitUntilIdle()
}
