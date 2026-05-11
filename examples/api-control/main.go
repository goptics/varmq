package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	// 1. Email Worker (Slow, low concurrency)
	emailWorker := varmq.NewWorker(func(j varmq.Job[string]) {
		time.Sleep(time.Duration(2+rand.Intn(3)) * time.Second)
	}, "email-sender")
	emailQueue := emailWorker.BindQueue()

	// 2. Image Processor (Medium speed, high concurrency)
	imageWorker := varmq.NewWorker(func(j varmq.Job[string]) {
		time.Sleep(time.Duration(500+rand.Intn(1000)) * time.Millisecond)
	}, "image-processor", 10, varmq.WithMinIdleWorkerRatio(50))
	imageQueue := imageWorker.BindQueue()

	// 3. Analytics Indexer (Very fast, bulk work)
	analyticsWorker := varmq.NewWorker(func(j varmq.Job[int]) {
		time.Sleep(50 * time.Millisecond)
	}, "analytics-indexer", 5)
	analyticsQueue := analyticsWorker.BindQueue()

	// 4. Heavy Cleanup Task (Extremely slow, single concurrency)
	cleanupWorker := varmq.NewWorker(func(j varmq.Job[string]) {
		time.Sleep(10 * time.Second)
	}, "system-cleanup")
	cleanupQueue := cleanupWorker.BindQueue()

	// Feed workers some initial and periodic work
	go func() {
		for {
			// Add 1-3 emails
			for i := 0; i < rand.Intn(3)+1; i++ {
				emailQueue.Add(fmt.Sprintf("email-%d", time.Now().UnixNano()))
			}
			// Add 10-20 images
			for i := 0; i < rand.Intn(10)+10; i++ {
				imageQueue.Add(fmt.Sprintf("img-%d.jpg", i))
			}
			// Add 50 analytics events
			for range 500 {
				analyticsQueue.Add(rand.Intn(1000))
			}
			// Add 1 cleanup task
			cleanupQueue.Add("periodic-flush")

			time.Sleep(15 * time.Second)
		}
	}()

	fmt.Println("Starting Demo Server on :8080...")
	fmt.Println("Endpoints available at /api")

	if err := http.ListenAndServe(":8080", varmq.Handler("/api")); err != nil {
		panic(err)
	}

	// now run vartop and set the api: http://localhost:8080/api
}
