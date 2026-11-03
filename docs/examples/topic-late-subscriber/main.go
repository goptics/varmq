package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	topic := varmq.NewTopic[int]()

	// Nobody is subscribed yet, so the payload has nowhere to go.
	fmt.Println("publish 1:", topic.Publish(1))

	email := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("email got", j.Data())
	})
	email.WithTopic(topic)
	topic.Publish(2)
	email.WaitUntilIdle()

	audit := varmq.NewWorker(func(j varmq.Job[int]) {
		time.Sleep(10 * time.Millisecond)
		fmt.Println("audit got", j.Data())
	})
	audit.WithTopic(topic)
	topic.Publish(3)

	email.WaitUntilIdle()
	audit.WaitUntilIdle()
}
