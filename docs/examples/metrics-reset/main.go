package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("ran job", j.Data())
	})
	queue := worker.BindQueue()
	m := worker.Metrics()

	queue.Add(1)
	queue.Add(2)
	worker.WaitUntilIdle()
	fmt.Println("before Reset: submitted", m.Submitted(), "completed", m.Completed())

	m.Reset()
	fmt.Println("after Reset: submitted", m.Submitted(), "completed", m.Completed())
}
