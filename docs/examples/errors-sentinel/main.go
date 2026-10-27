package main

import (
	"errors"
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("job", j.Data())
	}, 1)
	worker.BindQueue()

	err := worker.TunePool(1)
	fmt.Println("TunePool(1):", err)
	fmt.Println("is ErrSameConcurrency:", errors.Is(err, varmq.ErrSameConcurrency))

	worker.StopAndWait()
	err = worker.Resume()
	fmt.Println("Resume:", err)
	fmt.Println("is ErrWorkerStopped:", errors.Is(err, varmq.ErrWorkerStopped))
}
