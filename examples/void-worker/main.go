package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	w := varmq.NewWorker(func(data int) {
		fmt.Printf("Processing: %d\n", data)
		time.Sleep(1 * time.Second)
	}, 100)

	q := w.BindQueue()
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()
	defer q.WaitUntilFinished()

	fmt.Println("Added jobs")
}
