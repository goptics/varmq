package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	work := func(j varmq.Job[int]) {
		time.Sleep(100 * time.Millisecond)
		fmt.Println("job", j.Data(), "done")
	}

	stop := varmq.NewWorker(work)
	stopQueue := stop.BindQueue()
	for i := 1; i <= 3; i++ {
		stopQueue.Add(i)
	}
	time.Sleep(10 * time.Millisecond) // job 1 is running
	stop.StopAndWait()
	fmt.Println("StopAndWait:", stop.Status(), "- pending:", stop.NumPending())

	drain := varmq.NewWorker(work)
	drainQueue := drain.BindQueue()
	for i := 1; i <= 3; i++ {
		drainQueue.Add(i)
	}
	drain.WaitAndStop()
	drain.WaitUntilStopped()
	fmt.Println("WaitAndStop:", drain.Status(), "- pending:", drain.NumPending())
}
