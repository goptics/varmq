package main

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/goptics/varmq"
)

func main() {
	var handled atomic.Int32

	// A string after the function names the worker. Job 2 returns an error and
	// job 4 panics. The handler sees both.
	worker := varmq.NewErrWorker(func(j varmq.Job[int]) error {
		switch j.Data() {
		case 2:
			return errors.New("bad input")
		case 4:
			panic("boom")
		}
		return nil
	}, 1, "mailer", varmq.WithErrHandler(func(w varmq.Worker, err error) {
		handled.Add(1)
		fmt.Printf("handler (%s): %v\n", w.Name(), err)
	}))
	queue := worker.BindQueue()

	var jobs []varmq.EnqueuedErrJob
	for i := 1; i <= 5; i++ {
		job, _ := queue.Add(i)
		jobs = append(jobs, job)
	}
	worker.WaitUntilIdle()

	// Each job's handle has its own outcome.
	for i, job := range jobs {
		fmt.Printf("job %d: %v\n", i+1, job.Err())
	}

	// Nobody read Errs() while the jobs ran. It holds one error and dropped the rest.
	fmt.Println("handler calls:", handled.Load())
	fmt.Println("errors waiting on Errs():", len(worker.Errs()))
	fmt.Println("first on Errs():", <-worker.Errs())
	select {
	case err := <-worker.Errs():
		fmt.Println("second on Errs():", err)
	default:
		fmt.Println("no second error on Errs()")
	}

	// A sentinel error is matched with errors.Is.
	worker.StopAndWait()
	err := worker.Resume()
	fmt.Println("Resume:", err)
	fmt.Println("is ErrWorkerStopped:", errors.Is(err, varmq.ErrWorkerStopped))
}
