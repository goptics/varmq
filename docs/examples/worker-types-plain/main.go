package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println("worker got", j.Data())
	})
	queue := worker.BindQueue()

	job, _ := queue.Add("hello")
	job.Wait() // blocks until the job is done; there is no outcome to read
	fmt.Println("job done")
}
