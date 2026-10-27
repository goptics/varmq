package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewErrWorker(func(j varmq.Job[int]) error {
		if j.Data() == 2 {
			return fmt.Errorf("job %d failed", j.Data())
		}
		return nil
	}, 1)
	queue := worker.BindQueue()

	for i := 1; i <= 3; i++ {
		queue.Add(i)
	}
	worker.WaitUntilIdle()

	m := worker.Metrics()
	fmt.Println("submitted", m.Submitted())
	fmt.Println("completed", m.Completed())
	fmt.Println("successful", m.Successful())
	fmt.Println("failed", m.Failed())
}
