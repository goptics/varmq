package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	w := varmq.NewWorker(func(data int) {
		fmt.Println("Processing:", data)
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
