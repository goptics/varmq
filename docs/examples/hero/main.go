package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		time.Sleep(500 * time.Millisecond)
		fmt.Println("done job", j.Data())
	}, 2) // two jobs at a time
	queue := worker.BindQueue()

	for i := range 10 {
		queue.Add(i)
	}
	worker.WaitUntilIdle()
}
