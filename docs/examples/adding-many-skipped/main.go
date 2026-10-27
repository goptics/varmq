package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	// Not started yet, so we can look at the group before any job runs.
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println("running", j.ID(), j.Data())
	}, varmq.WithAutoRun(false))
	queue := worker.BindQueue()

	items := []varmq.Item[string]{
		{ID: "a", Data: "resize"},
		{ID: "b", Data: "upload"},
		{ID: "a", Data: "resize again"}, // "a" is still in the queue: skipped
	}
	group := queue.AddAll(items)
	fmt.Println("pending:", group.NumPending(), "of", len(items))

	worker.Start()
	group.Wait()
	fmt.Println("pending:", group.NumPending())
}
