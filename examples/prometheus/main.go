package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/goptics/varmq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Create a worker with 10 concurrent workers
	worker := varmq.NewWorker(
		func(j varmq.Job[string]) {
			fmt.Printf("Processing: %s\n", j.Data())
			time.Sleep(100 * time.Millisecond)
		},
		varmq.WithConcurrency(10),
	)

	// Bind to a standard queue
	queue := worker.BindQueue()

	// Register worker metrics collectors
	registerMetrics(worker, "foo_worker")

	// Expose the registered metrics via HTTP
	http.Handle("/metrics", promhttp.Handler())

	// Submit tasks in background
	go submitTasks(queue)

	// Start the server
	fmt.Println("Starting Prometheus metrics server on :8080")
	fmt.Println("Visit http://localhost:8080/metrics to view metrics")
	http.ListenAndServe(":8080", nil)
}

func registerMetrics(w varmq.Worker, namespace string) {
	// Worker pool metrics
	prometheus.MustRegister(
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "workers_processing",
				Help:      "Number of jobs currently being processed",
			},
			func() float64 {
				return float64(w.NumProcessing())
			},
		),
	)

	prometheus.MustRegister(
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "workers_concurrency",
				Help:      "Maximum number of concurrent workers",
			},
			func() float64 {
				return float64(w.NumConcurrency())
			},
		),
	)

	prometheus.MustRegister(
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "workers_idle",
				Help:      "Number of idle workers in the pool",
			},
			func() float64 {
				return float64(w.NumIdleWorkers())
			},
		),
	)

	// Queue metrics
	prometheus.MustRegister(
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "waiting_total",
				Help:      "Number of tasks waiting to be processed",
			},
			func() float64 {
				return float64(w.NumWaiting())
			}),
	)

	prometheus.MustRegister(
		prometheus.NewCounterFunc(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "submitted_total",
				Help:      "Total number of tasks submitted",
			},
			func() float64 {
				return float64(w.Metrics().Submitted())
			},
		),
	)

	prometheus.MustRegister(
		prometheus.NewCounterFunc(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "completed_total",
				Help:      "Total number of tasks that completed processing",
			},
			func() float64 {
				return float64(w.Metrics().Completed())
			},
		),
	)

	prometheus.MustRegister(
		prometheus.NewCounterFunc(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "successful_total",
				Help:      "Total number of tasks that completed successfully",
			},
			func() float64 {
				return float64(w.Metrics().Successful())
			},
		),
	)

	prometheus.MustRegister(
		prometheus.NewCounterFunc(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "failed_total",
				Help:      "Total number of tasks that failed (error or panic)",
			},
			func() float64 {
				return float64(w.Metrics().Failed())
			},
		),
	)
}

func submitTasks(queue varmq.Queue[string]) {
	// Submit 1000 tasks
	for i := range 1000 {
		queue.Add(fmt.Sprintf("Task #%d", i))
	}
}
