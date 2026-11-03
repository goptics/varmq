package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	started := make(chan int)
	release := make(chan struct{})

	// Each job says it started, then holds until the program lets go.
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		started <- j.Data()
		<-release
	}, 100)
	queue := worker.BindQueue()
	fmt.Println("idle goroutines before any job:", worker.NumIdleWorkers())

	for i := 1; i <= 3; i++ {
		queue.Add(i)
		<-started
	}

	fmt.Println("NumConcurrency:", worker.NumConcurrency())
	fmt.Println("NumProcessing:", worker.NumProcessing())
	fmt.Println("NumPending:", worker.NumPending())
	fmt.Println("NumIdleWorkers:", worker.NumIdleWorkers())

	close(release)
	worker.WaitUntilIdle()
}
