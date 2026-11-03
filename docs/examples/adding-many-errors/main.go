package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewErrWorker(func(j varmq.Job[int]) error {
		if j.Data() < 0 {
			// An error does not name its job, so put the ID in the message.
			return fmt.Errorf("%s: %d is negative", j.ID(), j.Data())
		}
		return nil
	})
	queue := worker.BindQueue()

	group := queue.AddAll([]varmq.Item[int]{
		{ID: "p", Data: 5},
		{ID: "q", Data: -1},
		{ID: "r", Data: 7},
		{ID: "s", Data: -2},
	})

	failed := 0
	for err := range group.Errs() {
		fmt.Println(err)
		failed++
	}
	fmt.Println(failed, "of 4 failed")
}
