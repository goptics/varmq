package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		time.Sleep(100 * time.Millisecond)
		fmt.Println("job", j.Data(), "done")
	})
	queue := worker.BindQueue()

	for i := 1; i <= 3; i++ {
		queue.Add(i)
	}
	time.Sleep(10 * time.Millisecond) // job 1 is running
	worker.Pause()

	worker.Wait() // returns once job 1 is done
	fmt.Println("Wait returned - running:", worker.NumProcessing(), "- pending:", worker.NumPending())

	go func() {
		time.Sleep(500 * time.Millisecond)
		fmt.Println("resume")
		worker.Resume()
	}()

	worker.WaitUntilIdle() // waits through the pause
	fmt.Println("WaitUntilIdle returned - pending:", worker.NumPending())
}
