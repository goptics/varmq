package main

import (
	"fmt"
	"os"
	"runtime/trace"
	"time"

	"github.com/fahimfaisaal/gocq"
)

func main() {
	start := time.Now()
	defer func() {
		fmt.Printf("Took %s\n", time.Since(start))
	}()
	f, err := os.Create("trace.out")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := trace.Start(f); err != nil {
		panic(err)
	}

	// Make sure trace is stopped before your program ends
	defer trace.Stop()

	q := gocq.NewVoidPriorityQueue(20, func(data int) {
		fmt.Printf("Started Worker %d\n", data)
		for i := 0; i < 1e10; i++ {
			// do nothing
		}
		fmt.Printf("Ended Worker %d\n", data)
	})
	defer q.WaitAndClose()

	items := make([]gocq.PQItem[int], 50)
	for i := 0; i < 50; i++ {
		items[i] = gocq.PQItem[int]{Value: i + 1, Priority: i % 10}
	}

	q.AddAll(items)
	fmt.Println("All tasks have been added")

	for i := 1000; i < 1030; i++ {
		q.Add(i+1, 0)
	}
	fmt.Println("All tasks have been added 2")
}
