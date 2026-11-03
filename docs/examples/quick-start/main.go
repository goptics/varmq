package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Printf("Processing %d\n", j.Data())
		time.Sleep(1 * time.Second)
	}) // default concurrency is 1
	defer worker.WaitUntilIdle()
	queue := worker.BindQueue()

	queue.Add(1)
	queue.Add(2)
}
