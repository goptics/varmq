package main

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/goptics/varmq"
)

// The attempt number travels in the job's data.
type fetch struct {
	URL     string
	Attempt int
}

const maxAttempts = 3

// get fails for "b.example" every time, and for "a.example" on the first two attempts.
func get(f fetch) error {
	if f.URL == "b.example" || f.Attempt < 3 {
		return errors.New("timeout")
	}
	return nil
}

func main() {
	var open sync.WaitGroup // one per URL, done when its last attempt ends

	// The dead-letter queue: a second queue for the jobs that used all their attempts.
	dead := varmq.NewWorker(func(j varmq.Job[fetch]) {
		fmt.Println("dead letter:", j.Data().URL)
		open.Done()
	})
	deadLetters := dead.BindQueue()

	var queue varmq.Queue[fetch]
	worker := varmq.NewWorker(func(j varmq.Job[fetch]) {
		f := j.Data()
		err := get(f)
		switch {
		case err == nil:
			fmt.Printf("%s: done on attempt %d\n", f.URL, f.Attempt)
			open.Done()
		case f.Attempt == maxAttempts:
			deadLetters.Add(f)
		default:
			backoff := time.Duration(f.Attempt) * 100 * time.Millisecond
			fmt.Printf("%s: attempt %d failed, retry in %v\n", f.URL, f.Attempt, backoff)
			f.Attempt++
			time.AfterFunc(backoff, func() { queue.Add(f) })
		}
	})
	queue = worker.BindQueue()

	open.Add(2)
	queue.Add(fetch{URL: "a.example", Attempt: 1})
	queue.Add(fetch{URL: "b.example", Attempt: 1})

	// WaitUntilIdle does not know about the timers, so wait for the outcomes.
	open.Wait()
}
