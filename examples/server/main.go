package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	gocmq "github.com/fahimfaisaal/gocmq"
)

type ScrapeResult struct {
	Status string
	Result string
}

var (
	queue = gocmq.NewWorker(scrapeWorker, gocmq.WithCache(new(sync.Map))).BindQueue()
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

	queue.Add(url, gocmq.WithJobId(jobID))
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Path[len("/scrape/status/"):]

	job, err := queue.JobById(jobID)

	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	bytes, err := job.Json()

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(bytes)
}

func main() {
	http.HandleFunc("/scrape/", scrapeHandler)
	http.HandleFunc("/scrape/status/", statusHandler)

	fmt.Println("Server is running on port 8080")
	http.ListenAndServe(":8080", nil)
}
