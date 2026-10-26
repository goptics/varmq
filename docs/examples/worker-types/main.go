package main

import (
	"fmt"
	"strconv"

	"github.com/goptics/varmq"
)

func main() {
	// NewWorker: the function returns nothing, so the handle has no outcome
	// to read. Wait blocks until the job is done.
	plain := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println("plain worker got", j.Data())
	}, 1)
	if job, ok := plain.BindQueue().Add("hello"); ok {
		job.Wait()
		fmt.Println("plain: job done")
	}

	// NewErrWorker: the function returns an error. Err blocks until the job
	// is done, then returns it.
	checker := varmq.NewErrWorker(func(j varmq.Job[int]) error {
		if j.Data()%2 != 0 {
			return fmt.Errorf("%d is odd", j.Data())
		}
		return nil
	}, 1)
	errQueue := checker.BindQueue()
	for _, n := range []int{2, 3} {
		// Add returns false only for a full or closed queue or a duplicate
		// ID. None applies here, so the examples skip the check.
		errJob, _ := errQueue.Add(n)
		fmt.Printf("err worker, job %d: %v\n", n, errJob.Err())
	}

	// NewResultWorker: the function returns a value and an error. Result
	// blocks until the job is done, then returns both.
	parser := varmq.NewResultWorker(func(j varmq.Job[string]) (int, error) {
		return strconv.Atoi(j.Data())
	}, 1)
	resultQueue := parser.BindQueue()
	for _, s := range []string{"42", "forty-two"} {
		resultJob, _ := resultQueue.Add(s)
		n, err := resultJob.Result()
		fmt.Printf("result worker, %q: %d, %v\n", s, n, err)
	}
}
