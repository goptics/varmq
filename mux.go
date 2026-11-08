package varmq

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"weak"
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

// workflowRegistry holds every known workflow store by ScopeID, for
// /workflows. Filled by NewWorkflowStore and when a queue binds. An
// in-memory store is held weakly, so dropping it frees it.
// ponytail: durable entries are never removed, like queueRegistry's; a
// closed storage stays: GET /workflows skips it, the ID routes answer 503
// when no other store holds the ID. One handle per durable scope is kept:
// a later bind of the same scope replaces the earlier entry, so closing the
// later handle loses the scope's workflows here; keep that handle open.
var workflowRegistry sync.Map

// registerWorkflowStore serves s under /workflows: GET /workflows lists it
// if it is a WorkflowInspector.
func registerWorkflowStore(s WorkflowStore) {
	switch m := s.(type) {
	case nil:
	case *memWorkflowStore: // every in-memory store shares one scope
		workflowRegistry.Store(fmt.Sprintf("%s#%p", m.ScopeID(), m), weak.Make(m))
	default:
		workflowRegistry.Store(s.ScopeID(), s)
	}
}

// workflowStores is every registered store still alive; the entry of a
// collected in-memory store is removed.
func workflowStores() []WorkflowStore {
	var out []WorkflowStore
	workflowRegistry.Range(func(k, v any) bool {
		switch s := v.(type) {
		case weak.Pointer[memWorkflowStore]:
			if m := s.Value(); m != nil {
				out = append(out, m)
			} else {
				workflowRegistry.CompareAndDelete(k, v)
			}
		case WorkflowStore:
			out = append(out, s)
		}
		return true
	})

	return out
}

// registerWorkflowStorage lists the workflow store of a queue storage.
func registerWorkflowStorage(q any) {
	if wa, ok := q.(WorkflowAcknowledgeable); ok {
		registerWorkflowStore(wa.WorkflowStore())
	}
}

// maxWorkflowsOffset caps GET /workflows' offset: every store pages
// offset+limit records, so an unbounded offset is unbounded work.
const maxWorkflowsOffset = 10000

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

// workflowStates are the states GET /workflows filters by.
var workflowStates = []WorkflowState{WorkflowRunning, WorkflowCompleted, WorkflowFailed,
	WorkflowCompensating, WorkflowCompensated, WorkflowCompensationFailed, WorkflowCancelled}

// handleListWorkflows returns one page of workflows from every registered
// WorkflowInspector, newest first. A store whose listing (or a read of a
// listed record) fails is skipped; 503 only when every one fails.
func handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	state := WorkflowState(query.Get("state"))
	if state != "" && !slices.Contains(workflowStates, state) {
		writeError(w, http.StatusBadRequest, "unknown state")
		return
	}

	offset, err := queryInt(query.Get("offset"), 0)
	if err != nil || offset < 0 || offset > maxWorkflowsOffset {
		writeError(w, http.StatusBadRequest, "invalid offset")
		return
	}

	limit, err := queryInt(query.Get("limit"), defaultJobsLimit)
	if err != nil || limit < 1 || limit > maxJobsLimit {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
	}

	// each store may hold the whole page, so every one gives offset+limit
	want := offset + limit
	var (
		all    []listed
		failed error
		down   []bool // per store: its listing or a read failed
		ndown  int
	)
	for _, ws := range workflowStores() {
		if _, ok := ws.(WorkflowInspector); !ok {
			continue
		}
		si := len(down)
		down = append(down, false)
		got, err := listWorkflows(r, ws, si, state, want)
		if err != nil {
			failed, down[si], ndown = err, true, ndown+1
			continue
		}
		all = append(all, got...)
	}

	if ndown > 0 && ndown == len(down) {
		writeError(w, http.StatusServiceUnavailable, failed.Error())
		return
	}

	// ponytail: O(stores x (offset+limit)) keys per call; a cursor if deep pages matter.
	slices.SortStableFunc(all, func(a, b listed) int {
		if c := cmp.Compare(b.key.CreatedAt, a.key.CreatedAt); c != 0 {
			return c
		}

		return strings.Compare(a.key.ID, b.key.ID)
	})

	page := []WorkflowRecord{}
	for _, l := range all[min(offset, len(all)):min(want, len(all))] {
		if down[l.si] {
			continue
		}
		if l.rec == nil {
			rec, err := l.ws.Record(r.Context(), l.key.ID)
			if errors.Is(err, ErrWorkflowNotFound) { // swept since the listing
				continue
			}
			if err != nil { // the store failed since the listing
				failed, down[l.si], ndown = err, true, ndown+1
				continue
			}
			l.rec = &rec
		}
		page = append(page, *l.rec)
	}

	if ndown > 0 && ndown == len(down) {
		writeError(w, http.StatusServiceUnavailable, failed.Error())
		return
	}

	writeJSON(w, http.StatusOK, page)
}

// listed is one workflow of a store's listing: its sort key, its store
// (and the store's index in the listing), and its record when the store
// listed records.
type listed struct {
	key WorkflowKey
	ws  WorkflowStore
	si  int
	rec *WorkflowRecord
}

// listWorkflows lists ws's first n workflows in state: keys only when ws is
// a WorkflowKeyLister, else whole records. ws is a WorkflowInspector; si
// is its index.
func listWorkflows(r *http.Request, ws WorkflowStore, si int, state WorkflowState, n int) ([]listed, error) {
	if kl, ok := ws.(WorkflowKeyLister); ok {
		keys, err := kl.WorkflowKeys(r.Context(), state, 0, n)
		out := make([]listed, len(keys))
		for i, k := range keys {
			out[i] = listed{key: k, ws: ws, si: si}
		}
		return out, err
	}

	recs, err := ws.(WorkflowInspector).Workflows(r.Context(), state, 0, n)
	out := make([]listed, len(recs))
	for i := range recs {
		out[i] = listed{key: WorkflowKey{ID: recs[i].ID, CreatedAt: recs[i].CreatedAt}, ws: ws, si: si, rec: &recs[i]}
	}
	return out, err
}

// errWorkflowAmbiguous: two registered stores hold the same workflow ID.
var errWorkflowAmbiguous = errors.New("workflow id in several stores")

// findWorkflow returns the registered store that holds workflow id, and its
// record. ErrWorkflowNotFound when none does or id is not a workflow ID (no
// store is asked: it keeps "index" from reaching a store);
// errWorkflowAmbiguous when several do; else the store error when some
// store failed and none hit.
func findWorkflow(r *http.Request, id string) (WorkflowStore, WorkflowRecord, error) {
	if !validWorkflowID(id) {
		return nil, WorkflowRecord{}, ErrWorkflowNotFound
	}

	var (
		found WorkflowStore
		rec   WorkflowRecord
		miss  = ErrWorkflowNotFound
	)
	for _, ws := range workflowStores() {
		got, err := ws.Record(r.Context(), id)
		switch {
		case err == nil && found != nil:
			return nil, WorkflowRecord{}, errWorkflowAmbiguous
		case err == nil:
			found, rec = ws, got
		case !errors.Is(err, ErrWorkflowNotFound):
			miss = err
		}
	}

	if found == nil {
		return nil, rec, miss
	}

	return found, rec, nil
}

// writeWorkflowErr maps a store error: 404 for not found, 409 for an ID
// in several stores, else 503.
func writeWorkflowErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrWorkflowNotFound):
		writeError(w, http.StatusNotFound, "workflow not found")
	case errors.Is(err, errWorkflowAmbiguous):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusServiceUnavailable, err.Error())
	}
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

	// absent: the workflow's own WithUndoOnCancel. REST may only narrow:
	// undo=true would run undos the producer turned off (the Go API may).
	var undo *bool
	switch r.URL.Query().Get("undo") {
	case "":
	case "false":
		undo = new(false)
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
	pokeLocal(undoQueueIDs(rec)) // as Workflow.Cancel: wake local undo consumers

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
