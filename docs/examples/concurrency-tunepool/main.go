package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	started := make(chan int, 6) // room for every job, so none blocks here
	release := make(chan struct{})

	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		started <- j.Data()
		<-release
	})
	queue := worker.BindQueue()

	queue.Add(1)
	<-started // job 1 is running
	fmt.Println("TunePool(3):", worker.TunePool(3))

	for i := 2; i <= 6; i++ {
		queue.Add(i)
	}
	<-started
	<-started // jobs 2 and 3 started beside job 1
	fmt.Println("processing", worker.NumProcessing(), "pending", worker.NumPending())

	fmt.Println("TunePool(3) again:", worker.TunePool(3))

	close(release)
	worker.WaitUntilIdle()
}
