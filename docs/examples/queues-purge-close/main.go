package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(
		func(j varmq.Job[string]) {
			fmt.Println("running", j.Data())
		},
		varmq.WithAutoRun(false),
	)

	queue := worker.BindQueue()

	a, _ := queue.Add("a")
	b, _ := queue.Add("b")
	fmt.Println("Len:", queue.Len())

	queue.Purge()
	fmt.Println("after Purge, Len:", queue.Len(), "- a:", a.Status(), "- b:", b.Status())

	queue.Add("c")
	queue.Close()
	_, ok := queue.Add("d")
	fmt.Println("after Close, add d:", ok)

	// "c" was added before Close, so it still runs.
	worker.Start()
	worker.WaitUntilIdle()
}
