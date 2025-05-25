package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	w := varmq.NewErrWorker(func(data int) error {
		fmt.Printf("Processing: %d\n", data)
		time.Sleep(1 * time.Second)
		return nil
	}, 100)

	q := w.BindQueue()
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()
	defer q.WaitAndClose()

	fmt.Println("Added jobs")
}
