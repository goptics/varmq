package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	started := make(chan int, 3)
	gate := make(chan struct{})

	// Each job reports that it started, then waits for one value on gate.
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		started <- j.Data()
		<-gate
		fmt.Println("job", j.Data(), "done")
	}, 1)
	queue := worker.BindQueue()

	for i := 1; i <= 3; i++ {
		queue.Add(i)
	}
	<-started // job 1 is running
	fmt.Println("running:", worker.NumProcessing(), "- pending:", worker.NumPending())

	// Pause returns at once. Job 1 is still running, so the worker is Pausing.
	worker.Pause()
	fmt.Println("after Pause:", worker.Status())

	gate <- struct{}{} // let job 1 finish
	worker.WaitUntilPaused()
	fmt.Println("after WaitUntilPaused:", worker.Status(), "- pending:", worker.NumPending())

	// Wait returns as soon as no job is running, although two jobs still wait.
	worker.Wait()
	fmt.Println("Wait returned - pending:", worker.NumPending())

	// WaitUntilIdle needs the Idle status, which a paused worker does not have.
	idle := make(chan struct{})
	go func() {
		worker.WaitUntilIdle()
		close(idle)
	}()
	select {
	case <-idle:
		fmt.Println("WaitUntilIdle returned")
	case <-time.After(100 * time.Millisecond):
		fmt.Println("WaitUntilIdle is still waiting - the worker is", worker.Status())
	}

	worker.Resume()
	<-started
	gate <- struct{}{} // job 2
	<-started
	gate <- struct{}{} // job 3
	<-idle
	fmt.Println("after Resume, WaitUntilIdle returned:", worker.Status())

	// WaitAndStop runs everything still queued, then starts the stop and returns.
	// The worker can still be Stopping at that point, so WaitUntilStopped confirms it.
	drain := varmq.NewWorker(func(j varmq.Job[int]) {
		time.Sleep(50 * time.Millisecond)
		fmt.Println("drain: job", j.Data(), "done")
	}, 1)
	drainQueue := drain.BindQueue()
	for i := 1; i <= 3; i++ {
		drainQueue.Add(i)
	}
	drain.WaitAndStop()
	drain.WaitUntilStopped()
	fmt.Println("after WaitAndStop:", drain.Status(), "- pending:", drain.NumPending())

	// StopAndWait starts the stop at once and blocks until the worker is Stopped.
	// The running job ends, and the jobs that had not started stay queued.
	// This job waits on the worker's context, which Stop cancels.
	running := make(chan struct{}, 1)
	var stopper varmq.IWorkerBinder[int]
	stopper = varmq.NewWorker(func(j varmq.Job[int]) {
		running <- struct{}{}
		<-stopper.Context().Done()
		fmt.Println("stop: job", j.Data(), "ended")
	}, 1)
	stopQueue := stopper.BindQueue()
	for i := 1; i <= 3; i++ {
		stopQueue.Add(i)
	}
	<-running // job 1 is running
	stopper.StopAndWait()
	fmt.Println("after StopAndWait:", stopper.Status(), "- pending:", stopper.NumPending())
}
