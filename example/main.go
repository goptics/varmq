package main

import (
	"fmt"
	"time"

	"github.com/fahimfaisaal/gocq"
)

func main() {
	// Create a queue with 2 concurrent workers
	queue := gocq.NewQueue(2, func(data int) (int, error) {
		time.Sleep(100 * time.Millisecond)
		if data == 10 {
			return 0, fmt.Errorf("error")
		}
		return data * 2, nil
	})
	defer queue.Close()

	// Add a single job
	result, err := queue.Add(5).Wait()

	fmt.Println(result)

	fmt.Println(result, err)
}
