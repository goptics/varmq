package main

import (
	"fmt"
	"time"

	"github.com/fahimfaisaal/varmq"
)

func main() {
	// Create a worker that processes strings and returns their length
	worker := varmq.NewWorker(func(data string) (int, error) {
		fmt.Println("Processing:", data)
		time.Sleep(1 * time.Second) // Simulate work
		return len(data), nil
	})

	// Bind to a standard queue
	queue := worker.BindQueue()
	defer queue.WaitAndClose() // Wait for all jobs to complete and close the queue

	// Add jobs to the queue
	job1 := queue.Add("Hello")
	job2 := queue.Add("World")

	// Get results (Result() returns both value and error)
	result1, err1 := job1.Result()
	if err1 != nil {
		fmt.Println("Error processing job1:", err1)
	} else {
		fmt.Println("Result 1:", result1)
	}

	result2, err2 := job2.Result()
	if err2 != nil {
		fmt.Println("Error processing job2:", err2)
	} else {
		fmt.Println("Result 2:", result2)
	}

	// Add multiple jobs at once
	items := []varmq.Item[string]{
		{Value: "Concurrent", ID: "1"},
		{Value: "Queue", ID: "2"},
	}

	groupJob := queue.AddAll(items)

	resultChan, err := groupJob.Results()
	if err != nil {
		fmt.Println("Error getting results channel:", err)
		return
	}

	// Get results as they complete in real-time
	for result := range resultChan {
		if result.Err != nil {
			fmt.Printf("Error processing job %s: %v\n", result.JobId, result.Err)
		} else {
			fmt.Printf("Result for job %s: %v\n", result.JobId, result.Data)
		}
	}
}
