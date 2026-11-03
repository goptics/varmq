package main

import (
	"fmt"
	"strconv"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewResultWorker(func(j varmq.Job[string]) (int, error) {
		return strconv.Atoi(j.Data())
	}, 3)
	queue := worker.BindQueue()

	group := queue.AddAll([]varmq.Item[string]{
		{ID: "a", Data: "10"},
		{ID: "b", Data: "x"},
		{ID: "c", Data: "30"},
	})

	for r := range group.Results() {
		if r.Err != nil {
			fmt.Println(r.JobId, "failed:", r.Err)
			continue
		}
		fmt.Println(r.JobId, "=", r.Data)
	}
	fmt.Println("pending:", group.NumPending())
}
