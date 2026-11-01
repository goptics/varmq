package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	topic := varmq.NewTopic[int](varmq.WithTopicCapacity(2))

	// Not started, so it reads nothing and payloads pile up unread.
	worker := varmq.NewWorker(
		func(j varmq.Job[int]) {
			fmt.Println("got", j.Data())
		},
		varmq.WithAutoRun(false),
	)
	worker.WithTopic(topic)

	fmt.Println("publish 1:", topic.Publish(1))
	fmt.Println("publish 2:", topic.Publish(2))
	fmt.Println("publish 3:", topic.Publish(3), "- IsFull:", topic.IsFull())

	worker.Start()
	worker.WaitUntilIdle()

	ok := topic.Publish(3)
	worker.WaitUntilIdle()
	fmt.Println("publish 3 again:", ok)
}
