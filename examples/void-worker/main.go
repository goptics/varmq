package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func workerFunc(item string) (string, error) {
	fmt.Printf("Processing: %s\n", item)
	time.Sleep(1 * time.Second)
	return fmt.Sprintf("Processed: %s\n", item), nil
}

func main() {
	start := time.Now()
	defer func() {
		fmt.Println("Time taken:", time.Since(start).Milliseconds(), "ms")
	}()

	w := varmq.NewWorker(workerFunc)
	q := w.BindQueue()
	defer q.WaitUntilFinished()
	defer fmt.Println("Added jobs")

	job := q.Add(fmt.Sprintf("Job %d\n", 1))
	job2 := q.Add(fmt.Sprintf("Job %d\n", 2))
	result, _ := job.Result()
	fmt.Println(result)
	result, _ = job2.Result()
	fmt.Println(result)

	for i := range 10 {
		q.Add(fmt.Sprintf("Task %d\n", i))
	}

	items := make([]varmq.Item[string], 0)
	for i := range 10 {
		items = append(items, varmq.Item[string]{Value: fmt.Sprintf("Job %d\n", i)})
	}

	groupJob := q.AddAll(items)
	groupCh, _ := groupJob.Results()

	for result := range groupCh {
		fmt.Println(result.Data)
	}
}
