package main

// Using the sqliteq adapter for SQLite persistence
import (
	"fmt"
	"time"

	"github.com/goptics/sqliteq"
	"github.com/goptics/varmq"
)

func main() {
	// Connect to SQLite database using the adapter
	sdb := sqliteq.New("test.db")

	// Create a persistent queue with optional configuration
	persistentQueue, err := sdb.NewQueue("test")

	if err != nil {
		panic(err)
	}

	// Create a worker
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Printf("Processing: %s\n", j.Data())
		time.Sleep(1 * time.Second)
		fmt.Printf("Processed: %s\n", j.Data())
	})

	// Bind the worker to the persistent queue
	queue := worker.WithPersistentQueue(persistentQueue)
	defer worker.WaitUntilFinished()

	for i := range 10 {
		queue.Add(fmt.Sprintf("Task %d", i))
	}

}
