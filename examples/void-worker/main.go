package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"
)

func main() {
	// w := varmq.NewWorker(func(data int) {
	// 	fmt.Printf("Processing: %d\n", data)
	// 	time.Sleep(1 * time.Second)
	// }, 100)

	pool, _ := ants.NewPool(100)
	wg := sync.WaitGroup{}
	// q := w.BindQueue()
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start))
	}()
	// defer q.WaitAndClose()

	for i := range 1000 {
		wg.Add(1)
		pool.Submit(func() {
			fmt.Printf("Processing: %d\n", i)
			time.Sleep(1 * time.Second)
			wg.Done()
		})
	}
	fmt.Println("Added jobs")
	wg.Wait()
}
