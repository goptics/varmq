package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	started := make(chan struct{}, 4)
	release := make(chan struct{})

	// WithAutoRun(false) creates the worker without starting it.
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		started <- struct{}{}
		<-release // the first job waits here, so the worker stays busy while we look
		fmt.Printf("job %d done\n", j.Data())
	}, 1, varmq.WithAutoRun(false))
	queue := worker.BindQueue()

	fmt.Println("created:", worker.Status())

	worker.Start()
	fmt.Println("Start:", worker.Status())

	for i := 1; i <= 3; i++ {
		queue.Add(i)
	}
	<-started // job 1 is running

	// Pause does not interrupt job 1. The worker is Pausing until it ends.
	worker.Pause()
	fmt.Println("Pause:", worker.Status())

	close(release) // let job 1 finish
	worker.PauseAndWait()
	fmt.Println("paused:", worker.Status(), "- IsPaused:", worker.IsPaused(), "- IsActive:", worker.IsActive())
	fmt.Println("jobs still waiting:", worker.NumPending())

	worker.Resume()
	worker.WaitUntilIdle() // jobs 2 and 3 run
	fmt.Println("Resume, then drained:", worker.Status(), "- IsIdle:", worker.IsIdle())

	worker.StopAndWait()
	fmt.Println("StopAndWait:", worker.Status(), "- IsStopped:", worker.IsStopped())

	// A stopped worker cannot be resumed.
	fmt.Println("Resume:", worker.Resume())

	// A job added while it is stopped waits in the queue.
	queue.Add(4)
	worker.Restart()
	worker.WaitUntilIdle() // job 4 runs
	fmt.Println("Restart, then drained:", worker.Status())
}
