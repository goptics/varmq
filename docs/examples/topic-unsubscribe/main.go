package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	topic := varmq.NewTopic[int]()

	email := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("email got", j.Data())
	})

	stock := varmq.NewWorker(func(j varmq.Job[int]) {
		time.Sleep(10 * time.Millisecond)
		fmt.Println("stock got", j.Data())
	})

	email.WithTopic(topic)
	stockSub := stock.WithTopic(topic)

	topic.Publish(1)
	email.WaitUntilIdle()
	stock.WaitUntilIdle()

	stockSub.Unsubscribe()
	fmt.Println("subscribers:", topic.NumSubscribers())

	topic.Publish(2)
	email.WaitUntilIdle()
	stock.WaitUntilIdle()
}
