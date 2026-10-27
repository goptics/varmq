package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewErrWorker(func(j varmq.Job[int]) error {
		if j.Data()%2 != 0 {
			return fmt.Errorf("%d is odd", j.Data())
		}
		return nil
	}, 1)
	queue := worker.BindQueue()

	for _, n := range []int{2, 3} {
		job, _ := queue.Add(n)
		fmt.Printf("job %d: %v\n", n, job.Err())
	}
}
