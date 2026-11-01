package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println("running", j.ID(), j.Data())
		time.Sleep(500 * time.Millisecond)
	})
	queue := worker.BindQueue()

	first, _ := queue.Add("resize photo", varmq.WithJobId("job-1"))
	second, _ := queue.Add("send email", varmq.WithJobId("job-2"))
	third, _ := queue.Add("build report", varmq.WithJobId("job-3"))
	time.Sleep(100 * time.Millisecond) // job-1 is running, the others wait

	fmt.Println("close job-1:", first.Close())
	fmt.Println("close job-2:", second.Close())
	second.Wait() // returns at once: job-2 is closed
	fmt.Println("job-2 is", second.Status())
	fmt.Println("close job-2 again:", second.Close())

	third.Wait() // the worker skips job-2 and runs job-3
}
