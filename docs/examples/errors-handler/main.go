package main

import (
	"errors"
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewErrWorker(func(j varmq.Job[int]) error {
		switch j.Data() {
		case 2:
			return errors.New("bad input")
		case 3:
			panic("boom")
		}
		return nil
	}, 1, "mailer", varmq.WithErrHandler(func(w varmq.Worker, err error) {
		fmt.Printf("handler (%s): %v\n", w.Name(), err)
	}))
	queue := worker.BindQueue()

	var jobs []varmq.EnqueuedErrJob
	for i := 1; i <= 3; i++ {
		job, _ := queue.Add(i)
		jobs = append(jobs, job)
	}
	worker.WaitUntilIdle()

	for i, job := range jobs {
		fmt.Printf("job %d: %v\n", i+1, job.Err())
	}
}
