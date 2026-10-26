package main

import (
	"errors"
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	// Func: the job's data is a func(), and the worker calls it.
	runner := varmq.NewWorker(varmq.Func(), 1)
	queue := runner.BindQueue()
	for i := 1; i <= 3; i++ {
		queue.Add(func() { fmt.Println("closure", i, "ran") })
	}
	runner.WaitUntilIdle()

	// ErrFunc: the data is a func() error. Err returns what it returned.
	checks := varmq.NewErrWorker(varmq.ErrFunc(), 1).BindQueue()
	good, _ := checks.Add(func() error { return nil })
	bad, _ := checks.Add(func() error { return errors.New("disk full") })
	fmt.Println("ErrFunc, first:", good.Err())
	fmt.Println("ErrFunc, second:", bad.Err())

	// ResultFunc[R]: the data is a func() (R, error). Result returns both.
	fetches := varmq.NewResultWorker(varmq.ResultFunc[string](), 1).BindQueue()
	fetch, _ := fetches.Add(func() (string, error) { return "payload", nil })
	body, err := fetch.Result()
	fmt.Printf("ResultFunc: %q, %v\n", body, err)

	// A nil func is not a crash: the job fails with an error.
	empty, _ := checks.Add(nil)
	fmt.Println("nil func:", empty.Err())
}
