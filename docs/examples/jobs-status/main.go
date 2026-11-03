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
	time.Sleep(100 * time.Millisecond) // job-1 has started by now

	fmt.Println(first.ID(), first.Status())
	fmt.Println(second.ID(), second.Status())

	first.Wait()
	second.Wait()
	fmt.Println(first.ID(), first.Status(), first.IsClosed())
	fmt.Println(second.ID(), second.Status(), second.IsClosed())
}
