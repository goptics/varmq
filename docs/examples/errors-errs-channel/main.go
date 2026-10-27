package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewErrWorker(func(j varmq.Job[int]) error {
		return fmt.Errorf("job %d failed", j.Data())
	}, 1)
	queue := worker.BindQueue()

	for i := 1; i <= 3; i++ {
		queue.Add(i)
	}
	worker.WaitUntilIdle() // nothing read Errs() while the jobs ran

	fmt.Println("waiting on Errs():", len(worker.Errs()))
	fmt.Println("first:", <-worker.Errs())

	select {
	case err := <-worker.Errs():
		fmt.Println("second:", err)
	default:
		fmt.Println("no second error")
	}
}
