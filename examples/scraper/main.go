package main

import (
	"fmt"
	"time"

	gocmq "github.com/fahimfaisaal/gocmq"
)

func main() {
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()

	w := gocmq.NewVoidWorker(func(data int) {
		fmt.Printf("Processing: %d\n", data)
		time.Sleep(1 * time.Second)
		fmt.Printf("Processed: %d\n", data)
	}, 100)

	q := w.BindQueue()
	defer q.WaitAndClose()

	for i := range 1000 {
		q.Add(i)
	}

	fmt.Println("added jobs")
}
