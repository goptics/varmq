package main

import (
	"errors"
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	started := make(chan int, 6)
	release := make(chan struct{})

	// Every job reports that it started, then waits on release. That keeps jobs
	// running while the program looks at the worker.
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		started <- j.Data()
		<-release
	}, 1)
	queue := worker.BindQueue()
	show("start", worker)

	queue.Add(1)
	<-started // job 1 is running
	show("limit 1", worker)

	// Raise the limit while job 1 is still running. The jobs added next can use it.
	fmt.Println("TunePool(3):", worker.TunePool(3))
	for i := 2; i <= 6; i++ {
		queue.Add(i)
	}
	<-started
	<-started // jobs 2 and 3 started beside job 1
	show("limit 3", worker)

	// Asking for the limit the worker already has is an error.
	err := worker.TunePool(3)
	fmt.Println("TunePool(3) again:", err)
	fmt.Println("is ErrSameConcurrency:", errors.Is(err, varmq.ErrSameConcurrency))

	close(release) // let every job finish
	worker.WaitUntilIdle()
	fmt.Println("all six jobs done")
}

func show(label string, w varmq.Worker) {
	fmt.Printf("%s: NumConcurrency %d, NumProcessing %d, NumPending %d, NumIdleWorkers %d\n",
		label, w.NumConcurrency(), w.NumProcessing(), w.NumPending(), w.NumIdleWorkers())
}
