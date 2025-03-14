package main

import (
	"fmt"
	"time"

	"github.com/fahimfaisaal/gocq"
)

func main() {

	// Create a queue with 2 concurrent workers
	queue := gocq.NewQueue(1, func(data int) (int, error) {
		time.Sleep(100 * time.Millisecond)
		if data == 10 {
			return 0, fmt.Errorf("error")
		}
		return data * 2, nil
	}).Pause()
	defer queue.Close()

	// Add a single job
	job1 := queue.Add(5)
	job2 := queue.Add(10)
	job3 := queue.Add(15)
	job4 := queue.Add(20)

	fmt.Println("Pending count:", queue.PendingCount())
	fmt.Println(job1.State())
	queue.Resume()
	if result, err := job4.WaitForResult(); err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Result:", result)
	}

	if result, err := job3.WaitForResult(); err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Result:", result)
	}

	if result, err := job2.WaitForResult(); err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Result:", result)
	}

	fmt.Println(job1.State())
	if result, err := job1.WaitForResult(); err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Result:", result)
	}
	fmt.Println(job1.State())
}
