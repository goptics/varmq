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

	worker := func(data int) (int, error) {
		return data * 2, nil
	}

	q := gocq.NewPriorityQueue(1, worker).Pause()

	resp1, _ := q.Add(1, 2)
	resp2, _ := q.Add(2, 1)
	resp3, _ := q.Add(3, 0)

	q.Resume()

	fmt.Println(<-resp1)
	fmt.Println(<-resp2)
	fmt.Println(<-resp3)
}
