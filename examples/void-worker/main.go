package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()

	w := varmq.NewVoidWorker(func(data int) {
		fmt.Printf("Processing: %d\n", data)
		time.Sleep(1 * time.Second)
	}, 100)

	q := w.BindQueue()
	defer q.WaitAndClose()
	defer fmt.Println("Added jobs")

	for i := range 1000 {
		q.Add(i)
	}
}
