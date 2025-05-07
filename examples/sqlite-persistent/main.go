package main

// Using the sqliteq adapter for SQLite persistence
import (
	"fmt"
	"time"

	"github.com/goptics/sqliteq"
	"github.com/goptics/varmq"
	"github.com/lucsky/cuid"
)

func main() {
	// Connect to SQLite database using the adapter
	sqliteQueue := sqliteq.New("test.db")

	// Create a persistent queue with optional configuration
	persistentQueue, err := sqliteQueue.NewQueue("test", sqliteq.WithRemoveOnComplete(false))
	if err != nil {
		// Handle error
		return
	}

	// Create a worker
	worker := varmq.NewWorker(func(data string) (string, error) {
		time.Sleep(1 * time.Second)
		return fmt.Sprintf("Processed: %s", data), nil
	}, 2) // 2 concurrent workers

	// Bind the worker to the persistent queue
	queue := worker.WithPersistentQueue(persistentQueue)
	defer queue.WaitUntilFinished()

	items := make([]varmq.Item[string], 50)
	for i := range items {
		items[i] = varmq.Item[string]{
			Value: fmt.Sprintf("Hello %d", i),
			ID:    cuid.New(),
		}
	}

	// Add multiple jobs at once using AddAll
	results, err := queue.AddAll(items).Results()
	if err != nil {
		return
	}
	// Or get results for specific jobs
	for result := range results {
		if result.Err != nil {
			fmt.Printf("Job %s failed: %v\n", result.JobId, result.Err)
			continue
		}
		fmt.Printf("Job %s processed with result: %v\n", result.JobId, result.Data)
	}
}
