package varmq

import (
	"cmp"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// WorkerRegistry safely stores reference to all active workers by Name
var WorkerRegistry sync.Map

// queueRegistry holds every bound QueueInspector by its QueueID, so a queue
// bound to several workers is listed once.
// ponytail: storage entries are never removed, as queues are never unbound; a
// closed queue stays listed with the error its QueueStats returns. In-memory
// queues leave with their worker (addMemQueue).
var queueRegistry sync.Map

// registerQueue lists q under GET /queues if it is a QueueInspector.
func registerQueue(q any) {
	if qi, ok := q.(QueueInspector); ok {
		queueRegistry.Store(qi.QueueID(), qi)
	}
}

// workflowStore is a registered store: it keeps workflows and lists them.
type workflowStore interface {
	WorkflowStore
	WorkflowInspector
}

// workflowRegistry holds every known workflow store by ScopeID, for
// /workflows. Filled by NewWorkflowStore and when a queue binds.
// ponytail: entries are never removed, like queueRegistry's; a closed
// storage stays and its errors answer 503.
var workflowRegistry sync.Map

// registerWorkflowStore lists s under /workflows if it can also list.
func registerWorkflowStore(s WorkflowStore) {
	if ws, ok := s.(workflowStore); ok {
		workflowRegistry.Store(ws.ScopeID(), ws)
	}
}

// registerWorkflowStorage lists the workflow store of a queue storage.
func registerWorkflowStorage(q any) {
	if wa, ok := q.(WorkflowAcknowledgeable); ok {
		registerWorkflowStore(wa.WorkflowStore())
	}
}

// Default and largest page size of GET /queues/{id}/jobs.
const (
	defaultJobsLimit = 50
	maxJobsLimit     = 1000
)

// InstanceHeader is the response header carrying [InstanceID]. Clients use it
// to tell which process answered when several instances sit behind one URL
// (e.g. a load balancer).
const InstanceHeader = "X-Varmq-Instance"

// InstanceID identifies this process in every REST API response. It defaults
// to the hostname (the pod name on Kubernetes), or "" if that is unavailable;
// set it before serving when several instances share a host, and before
// building storage that reports Instance Records (it is read once there).
var InstanceID, _ = os.Hostname()

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
//
// Handler sets [InstanceHeader] on every response, including the 404 and 405
// replies ServeMux generates itself for unknown paths or methods.
//
// The PATCH routes change state and have no auth: protect Handler. The
// worker routes act on this process only, but PATCH /queues/{id}/actions
// pauses a shared queue for every process that consumes it, and the pause
// outlives restarts.
func Handler(prefix string) http.Handler {
	mux := http.StripPrefix(prefix, NewServerMux())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(InstanceHeader, InstanceID)
		mux.ServeHTTP(w, r)
	})
}

// NewServerMux returns a new [http.ServeMux] with all the REST API routes configured.
// The returned mux has no path prefix, so it can be used directly or mounted
// under any prefix using [Handler].
func NewServerMux() *http.ServeMux {
	mux := http.NewServeMux()
	// GET /health
	mux.HandleFunc("GET /health", handleHealthCheck)

	// GET /workers
	mux.HandleFunc("GET /workers", handleListWorkers)
	// GET /workers/{name}
	mux.HandleFunc("GET /workers/{name}", handleGetWorker)

	// PATCH /workers/{name}/actions/{action}
	mux.HandleFunc("PATCH /workers/{name}/actions/{action}", handleWorkerAction)
	// PATCH /workers/{name}/config/concurrency/{number}
	mux.HandleFunc("PATCH /workers/{name}/config/concurrency/{number}", handleWorkerConcurrency)

	// GET /queues
	mux.HandleFunc("GET /queues", handleListQueues)
	// GET /queues/{id}/jobs?state=...&offset=...&limit=...
	mux.HandleFunc("GET /queues/{id}/jobs", handleListJobs)
	// PATCH /queues/{id}/actions/{action}
	mux.HandleFunc("PATCH /queues/{id}/actions/{action}", handleQueueAction)

	// GET /workflows?state=...&offset=...&limit=...
	mux.HandleFunc("GET /workflows", handleListWorkflows)
	// GET /workflows/{id}
	mux.HandleFunc("GET /workflows/{id}", handleGetWorkflow)
	// PATCH /workflows/{id}/actions/{action}
	mux.HandleFunc("PATCH /workflows/{id}/actions/{action}", handleWorkflowAction)

	return mux
}

// handleHealthCheck returns a simple 200 OK status.
func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
		writeError(w, http.StatusNotFound, "worker not found")
		return
	}

	metrics := worker.Metrics()
	details := struct {
		workerSummary
		Metrics struct {
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

	details.Metrics.Completed = metrics.Completed()
	details.Metrics.Successful = metrics.Successful()
	details.Metrics.Failed = metrics.Failed()

	writeJSON(w, http.StatusOK, details)
}

// handleWorkerAction handles state-modifying actions on a worker (pause, resume, restart, stop).
func handleWorkerAction(w http.ResponseWriter, r *http.Request) {
	worker := lookupWorker(r.PathValue("name"))
	if worker == nil {
		writeError(w, http.StatusNotFound, "worker not found")
		return
	}

	action := r.PathValue("action")

	var err error
	switch action {
	case "pause":
		err = worker.Pause()
	case "restart":
		err = worker.Restart()
	case "resume":
		err = worker.Resume()
	case "start":
		err = worker.Start()
	case "stop":
		err = worker.Stop()
	default:
		writeError(w, http.StatusBadRequest, "unknown action")
		return
	}

	handleActionErr(w, err)
}

// handleWorkerConcurrency updates the worker's concurrency limit.
func handleWorkerConcurrency(w http.ResponseWriter, r *http.Request) {
	worker := lookupWorker(r.PathValue("name"))
	if worker == nil {
		writeError(w, http.StatusNotFound, "worker not found")
		return
	}

	concurrency, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid concurrency value")
		return
	}

	err = worker.TunePool(concurrency)
	handleActionErr(w, err)
}

// queueSummary is one entry of GET /queues: the stats, or the error that
// kept them from being read.
type queueSummary struct {
	ID string `json:"id"`
	*QueueStats
	Error string `json:"error,omitempty"`
}

// summarize reads one queue's entry of GET /queues.
func summarize(r *http.Request, id string, qi QueueInspector) queueSummary {
	q := queueSummary{ID: id}
	if stats, err := qi.QueueStats(r.Context()); err != nil {
		q.Error = err.Error()
	} else {
		q.QueueStats = &stats
	}

	return q
}

// handleListQueues returns the stats of every bound QueueInspector.
func handleListQueues(w http.ResponseWriter, r *http.Request) {
	queues := make([]queueSummary, 0)

	queueRegistry.Range(func(key, value any) bool {
		queues = append(queues, summarize(r, key.(string), value.(QueueInspector)))

		return true
	})

	slices.SortFunc(queues, func(a, b queueSummary) int { return strings.Compare(a.ID, b.ID) })
	writeJSON(w, http.StatusOK, queues)
}

// handleQueueAction pauses or resumes a queue (QueuePauser) and returns its
// entry of GET /queues once the flag is stored.
func handleQueueAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	v, ok := queueRegistry.Load(id)
	if !ok {
		writeError(w, http.StatusNotFound, "queue not found")
		return
	}

	var err error
	switch r.PathValue("action") {
	case "pause":
		err = pauseQueue(v)
	case "resume":
		err = resumeQueue(v)
	default:
		writeError(w, http.StatusBadRequest, "unknown action")
		return
	}

	switch {
	case errors.Is(err, ErrPauseUnsupported):
		writeError(w, http.StatusNotImplemented, err.Error())
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeJSON(w, http.StatusOK, summarize(r, id, v.(QueueInspector)))
	}
}

// handleListJobs returns one page of a queue's jobs in one Job State.
func handleListJobs(w http.ResponseWriter, r *http.Request) {
	v, ok := queueRegistry.Load(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "queue not found")
		return
	}

	query := r.URL.Query()
	state := JobState(query.Get("state"))
	switch state {
	case JobPending, JobProcessing, JobCompleted, JobFailed:
	default:
		writeError(w, http.StatusBadRequest, "state must be pending, processing, completed or failed")
		return
	}

	offset, err := queryInt(query.Get("offset"), 0)
	if err != nil || offset < 0 {
		writeError(w, http.StatusBadRequest, "invalid offset")
		return
	}

	limit, err := queryInt(query.Get("limit"), defaultJobsLimit)
	if err != nil || limit < 1 || limit > maxJobsLimit {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
	}

	jobs, err := v.(QueueInspector).Jobs(r.Context(), state, offset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if jobs == nil {
		jobs = []JobRecord{}
	}

	writeJSON(w, http.StatusOK, jobs)
}

// handleListWorkflows returns one page of workflows from every registered
// store, newest first.
func handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	state := WorkflowState(query.Get("state"))
	switch state {
	case "", WorkflowRunning, WorkflowCompleted, WorkflowFailed, WorkflowCompensating,
		WorkflowCompensated, WorkflowCompensationFailed, WorkflowCancelled:
	default:
		writeError(w, http.StatusBadRequest, "unknown state")
		return
	}

	offset, err := queryInt(query.Get("offset"), 0)
	if err != nil || offset < 0 {
		writeError(w, http.StatusBadRequest, "invalid offset")
		return
	}

	limit, err := queryInt(query.Get("limit"), defaultJobsLimit)
	if err != nil || limit < 1 || limit > maxJobsLimit {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
	}

	// each store may hold the whole page, so every one gives offset+limit
	want := math.MaxInt
	if offset <= math.MaxInt-limit {
		want = offset + limit
	}

	all := []WorkflowRecord{}
	var failed error
	workflowRegistry.Range(func(_, v any) bool {
		recs, err := v.(workflowStore).Workflows(r.Context(), state, 0, want)
		if err != nil {
			failed = err
			return false
		}

		all = append(all, recs...)
		return true
	})

	if failed != nil {
		writeError(w, http.StatusServiceUnavailable, failed.Error())
		return
	}

	// ponytail: O(stores x (offset+limit)) per call; a cursor if deep pages matter.
	slices.SortStableFunc(all, func(a, b WorkflowRecord) int {
		if c := cmp.Compare(b.CreatedAt, a.CreatedAt); c != 0 {
			return c
		}

		return strings.Compare(a.ID, b.ID)
	})

	if offset >= len(all) {
		all = all[:0]
	} else {
		all = all[offset:min(len(all), want)]
	}

	writeJSON(w, http.StatusOK, all)
}

// findWorkflow returns the registered store that holds workflow id, and its
// record. ErrWorkflowNotFound when none does; else the store error when some
// store failed and none hit.
func findWorkflow(r *http.Request, id string) (workflowStore, WorkflowRecord, error) {
	var (
		found  workflowStore
		rec    WorkflowRecord
		failed error
	)

	workflowRegistry.Range(func(_, v any) bool {
		ws := v.(workflowStore)
		got, err := ws.Record(r.Context(), id)
		switch {
		case err == nil:
			found, rec = ws, got
			return false
		case !errors.Is(err, ErrWorkflowNotFound):
			failed = err
		}

		return true
	})

	switch {
	case found != nil:
		return found, rec, nil
	case failed != nil:
		return nil, rec, failed
	}

	return nil, rec, ErrWorkflowNotFound
}

// writeWorkflowErr maps a store error: 404 for not found, else 503.
func writeWorkflowErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrWorkflowNotFound) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	writeError(w, http.StatusServiceUnavailable, err.Error())
}

// handleGetWorkflow returns one workflow.
func handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	_, rec, err := findWorkflow(r, r.PathValue("id"))
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, rec)
}

// handleWorkflowAction cancels a workflow and returns its fresh record.
func handleWorkflowAction(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("action") != "cancel" {
		writeError(w, http.StatusBadRequest, "unknown action")
		return
	}

	undo := true
	switch r.URL.Query().Get("undo") {
	case "", "true":
	case "false":
		undo = false
	default:
		writeError(w, http.StatusBadRequest, "invalid undo")
		return
	}

	id := r.PathValue("id")
	ws, _, err := findWorkflow(r, id)
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}

	if err := ws.Cancel(r.Context(), id, undo); err != nil {
		writeWorkflowErr(w, err)
		return
	}

	rec, err := ws.Record(r.Context(), id)
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, rec)
}

// queryInt parses s, or returns def when s is empty.
func queryInt(s string, def int) (int, error) {
	if s == "" {
		return def, nil
	}

	return strconv.Atoi(s)
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

	w.Header().Set(InstanceHeader, InstanceID)
	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set(InstanceHeader, InstanceID)
	w.Header().Set("Content-Type", "application/json")
	// Marshal first: a status already sent can't become a 500.
	body, err := json.Marshal(data)
	if err != nil {
		status, body = http.StatusInternalServerError, []byte(`{"error":"encode response"}`)
	}
	w.WriteHeader(status)
	w.Write(append(body, '\n'))
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
