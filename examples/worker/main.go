package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	w := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("Processing:", j.Data())
		time.Sleep(1 * time.Second)
	})
	q := w.BindQueue()
	defer q.WaitUntilFinished()

	for i := range 100 {
		q.Add(i)
	}

	time.Sleep(5 * time.Second)
	w.Pause()
}
