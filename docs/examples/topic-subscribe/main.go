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

	topic.Publish(1) // both workers get their own job for this order
	topic.Publish(2)

	email.WaitUntilIdle()
	stock.WaitUntilIdle()
}
