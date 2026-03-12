package varmq

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
)

// WorkerRegistry safely stores reference to all active workers by Name
var WorkerRegistry sync.Map

type workerSummary struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	Concurrency    int    `json:"concurrency"`
	NumProcessing  int    `json:"num_processing"`
	NumPending     int    `json:"num_pending"`
	NumIdleWorkers int    `json:"num_idle_workers"`
}

// Handler returns an [http.Handler] that serves the varmq REST API
// under the given prefix. This is a convenience wrapper around [NewServerMux].
//
// Example:
//
//	mainMux := http.NewServeMux()
//	mainMux.Handle("/varmq/", varmq.Handler("/varmq"))
//	http.ListenAndServe(":8080", mainMux)
func Handler(prefix string) http.Handler {
	return http.StripPrefix(prefix, NewServerMux())
}

// NewServerMux returns a new [http.ServeMux] with all the REST API routes configured.
// The returned mux has no path prefix, so it can be used directly or mounted
// under any prefix using [Handler].
func NewServerMux() *http.ServeMux {
	mux := http.NewServeMux()

	// GET /workers
	mux.HandleFunc("GET /workers", handleListWorkers)
	// GET /workers/{name}
	mux.HandleFunc("GET /workers/{name}", handleGetWorker)

	// PATCH /workers/{name}/actions/{action}
	mux.HandleFunc("PATCH /workers/{name}/actions/{action}", handleWorkerAction)
	// PATCH /workers/{name}/config/concurrency/{number}
	mux.HandleFunc("PATCH /workers/{name}/config/concurrency/{number}", handleWorkerConcurrency)

	return mux
}

// registerWorker stores the worker in the global registry.
func registerWorker(w Worker) {
	if w != nil {
		WorkerRegistry.Store(w.Name(), w)
	}
}

// handleListWorkers returns a high-level summary of all registered workers.
func handleListWorkers(w http.ResponseWriter, r *http.Request) {
	workers := make([]workerSummary, 0)

	WorkerRegistry.Range(func(key, value any) bool {
		if worker, ok := value.(Worker); ok {
			workers = append(workers, workerSummary{
				Name:           worker.Name(),
				Status:         worker.Status(),
				Concurrency:    worker.NumConcurrency(),
				NumProcessing:  worker.NumProcessing(),
				NumPending:     worker.NumPending(),
				NumIdleWorkers: worker.NumIdleWorkers(),
			})
		}

		return true // continue iteration
	})

	writeJSON(w, http.StatusOK, workers)
}

// lookupWorker retrieves a worker from the registry by name.
func lookupWorker(name string) Worker {
	v, ok := WorkerRegistry.Load(name)
	if !ok || v == nil {
		return nil
	}

	if w, ok := v.(Worker); ok {
		return w
	}

	return nil
}

// handleGetWorker returns detailed information for a specific worker.
func handleGetWorker(w http.ResponseWriter, r *http.Request) {
	worker := lookupWorker(r.PathValue("name"))
	if worker == nil {
		http.Error(w, "worker not found", http.StatusNotFound)
		return
	}

	metrics := worker.Metrics()
	details := struct {
		workerSummary
		Metrics struct {
			Submitted  uint64 `json:"submitted"`
			Completed  uint64 `json:"completed"`
			Successful uint64 `json:"successful"`
			Failed     uint64 `json:"failed"`
		} `json:"metrics"`
	}{
		workerSummary: workerSummary{
			Name:           worker.Name(),
			Status:         worker.Status(),
			Concurrency:    worker.NumConcurrency(),
			NumProcessing:  worker.NumProcessing(),
			NumPending:     worker.NumPending(),
			NumIdleWorkers: worker.NumIdleWorkers(),
		},
	}

	details.Metrics.Submitted = metrics.Submitted()
	details.Metrics.Completed = metrics.Completed()
	details.Metrics.Successful = metrics.Successful()
	details.Metrics.Failed = metrics.Failed()

	writeJSON(w, http.StatusOK, details)
}

// handleWorkerAction handles state-modifying actions on a worker (pause, resume, restart, stop).
func handleWorkerAction(w http.ResponseWriter, r *http.Request) {
	worker := lookupWorker(r.PathValue("name"))
	if worker == nil {
		http.Error(w, "worker not found", http.StatusNotFound)
		return
	}

	action := r.PathValue("action")

	var err error
	switch action {
	case "pause":
		err = worker.Pause()
	case "resume":
		err = worker.Resume()
	case "restart":
		err = worker.Restart()
	case "stop":
		err = worker.Stop()
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}

	handleActionErr(w, err)
}

// handleWorkerConcurrency updates the worker's concurrency limit.
func handleWorkerConcurrency(w http.ResponseWriter, r *http.Request) {
	worker := lookupWorker(r.PathValue("name"))
	if worker == nil {
		http.Error(w, "worker not found", http.StatusNotFound)
		return
	}

	concurrency, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		http.Error(w, "invalid concurrency value", http.StatusBadRequest)
		return
	}

	err = worker.TunePool(concurrency)
	handleActionErr(w, err)
}

func handleActionErr(w http.ResponseWriter, err error) {
	if err != nil {
		// map generic errors like "worker not running" to proper status codes
		if errors.Is(err, ErrNotRunningWorker) || errors.Is(err, ErrRunningWorker) || errors.Is(err, ErrSameConcurrency) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}

		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
