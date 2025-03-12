package main

import (
	"fmt"
	"time"

	"github.com/fahimfaisaal/gocq"
)

func main() {
	// Create a queue with 2 concurrent workers
	queue := gocq.NewPriorityQueue(1, func(data int) (int, error) {
		time.Sleep(100 * time.Millisecond)
		if data == 10 {
			return 0, fmt.Errorf("error")
		}
		return data * 2, nil
	}).Pause()
	defer queue.Close()

	// Add a single job
	job1 := queue.Add(5, 2)
	job2 := queue.Add(10, 1)
	job3 := queue.Add(15, 0)
	job4 := queue.Add(20, -1)

	queue.Resume()
	if result, err := job4.Wait(); err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("ResultChannel:", result)
	}

	if result, err := job3.Wait(); err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("ResultChannel:", result)
	}

	if result, err := job2.Wait(); err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("ResultChannel:", result)
	}

	if result, err := job1.Wait(); err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("ResultChannel:", result)
	}
}
