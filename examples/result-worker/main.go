package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	// Create a worker that processes strings and returns their length
	worker := varmq.NewResultWorker(func(j varmq.Job[string]) (int, error) {
		fmt.Println("Processing:", j.Data())
		time.Sleep(1 * time.Second) // Simulate work
		return len(j.Data()), nil
	})
	// Bind to a standard queue
	queue := worker.BindQueue()

	// Add jobs to the queue (non-blocking)
	if job, ok := queue.Add("Hello"); ok {
		fmt.Println("Job 1 added to queue.")

		go func() {
			// Get results (Result() returns both value and error)
			if result, err := job.Result(); err != nil {
				fmt.Println("Error processing job1:", err)
			} else {
				fmt.Println("Result 1:", result)
			}
		}()
	}

	if job, ok := queue.Add("World"); ok {
		fmt.Println("Job 2 added to queue.")

		go func() {
			// Get results (Result() returns both value and error)
			if result, err := job.Result(); err != nil {
				fmt.Println("Error processing job2:", err)
			} else {
				fmt.Println("Result 2:", result)
			}
		}()
	}

	// Add multiple jobs at once
	items := []varmq.Item[string]{
		{Data: "Hello", ID: "1"},
		{Data: "World", ID: "2"},
	}

	// Get results as they complete in real-time
	for result := range queue.AddAll(items).Results() {
		if result.Err != nil {
			fmt.Printf("Error processing job %s: %v\n", result.JobId, result.Err)
		} else {
			fmt.Printf("Result for job %s: %v\n", result.JobId, result.Data)
		}
	}
}
