package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/fahimfaisaal/gocq"
	"github.com/fahimfaisaal/gocq/types"
)

type ScrapeResult struct {
	Status string
}

var (
	queue      = gocq.NewQueue(10, scrapeWorker)
	jobResults = sync.Map{}
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func generateJobID() string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func scrapeWorker(url string) (string, error) {
	fmt.Printf("Scraping %s\n", url)
	time.Sleep(10 * time.Second)
	return fmt.Sprintf("Scraped content of %s", url), nil
}

func scrapeHandler(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Path[len("/scrape/"):]
	jobID := generateJobID()

	job := queue.Add(url)
	jobResults.Store(jobID, job)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Path[len("/scrape/status/"):]

	value, ok := jobResults.Load(jobID)
	if !ok {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	job := value.(types.EnqueuedJob[string])

	response := ScrapeResult{
		Status: job.Status(),
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func main() {
	http.HandleFunc("/scrape/", scrapeHandler)
	http.HandleFunc("/scrape/status/", statusHandler)

	fmt.Println("Server is running on port 8080")
	http.ListenAndServe(":8080", nil)
}
