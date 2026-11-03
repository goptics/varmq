package main

import (
	"fmt"
	"sync/atomic"

	"github.com/goptics/varmq"
)

func main() {
	var next atomic.Int32

	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Printf("%s has id %q\n", j.Data(), j.ID())
	}, varmq.WithJobIdGenerator(func() string {
		return fmt.Sprintf("auto-%d", next.Add(1))
	}))
	queue := worker.BindQueue()

	queue.Add("report")
	queue.Add("invoice", varmq.WithJobId("invoice-7"))
	queue.AddAll([]varmq.Item[string]{
		{ID: "a", Data: "item a"},
		{Data: "item b"},
	})

	worker.WaitUntilIdle()
}
