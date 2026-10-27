package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	// Not started, so nothing leaves the queue to make room.
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println("running", j.Data())
	}, varmq.WithAutoRun(false))

	queue := worker.BindQueue(varmq.WithQueueCapacity(2))

	_, ok := queue.Add("a")
	fmt.Println("add a:", ok)
	_, ok = queue.Add("b")
	fmt.Println("add b:", ok)

	job, ok := queue.Add("c")
	fmt.Println("add c:", ok, "- handle is nil:", job == nil)
	fmt.Println("Len:", queue.Len(), "- IsFull:", queue.IsFull())

	worker.Start()
	worker.WaitUntilIdle()
}
