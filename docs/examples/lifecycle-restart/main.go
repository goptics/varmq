package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("job", j.Data(), "done")
	}, 1)
	queue := worker.BindQueue()

	fmt.Println("Start:", worker.Start())

	worker.StopAndWait()
	fmt.Println("stopped:", worker.Status())
	fmt.Println("Resume:", worker.Resume())

	queue.Add(1)
	fmt.Println("added while stopped - pending:", worker.NumPending())

	worker.Restart()
	worker.WaitUntilIdle()
	fmt.Println("restarted:", worker.Status())
}
