package varmq

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/goptics/varmq/contract"
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

// memWorkflows holds every in-memory workflow store weakly, for /workflows:
// dropping a store frees it. Durable stores live in the contract registry.
var memWorkflows sync.Map

// registerMemWorkflowStore serves m under /workflows.
func registerMemWorkflowStore(m *memWorkflowStore) {
	memWorkflows.Store(fmt.Sprintf("%s#%p", m.ScopeID(), m), weak.Make(m))
}

// workflowStores is every registered durable store (in-memory stores must
// not be registered with contract) plus the in-memory ones
// still alive; the entry of a collected in-memory store is removed.
func workflowStores() []WorkflowStore {
	var out []WorkflowStore
	for _, s := range contract.WorkflowStores() {
		if _, mem := s.(*memWorkflowStore); !mem { // listed below, once
			out = append(out, s)
		}
	}
	memWorkflows.Range(func(k, v any) bool {
		if m := v.(weak.Pointer[memWorkflowStore]).Value(); m != nil {
			out = append(out, m)
		} else {
			memWorkflows.CompareAndDelete(k, v)
		}
		return true
	})

	return out
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
	// QueueIDs are the GET /queues IDs of the queues the worker consumes,
	// in-memory ones too, sorted. Empty when it binds none with an ID.
	QueueIDs []string `json:"queue_ids"`
}

// workerSource is one source a worker pulls from, in GET /workers/{name}.
type workerSource struct {
	// QueueID is the GET /queues ID, "" when the source has none (a topic
	// subscription, or storage with no Queue ID).
	QueueID string `json:"queue_id"`
	// Kind is "queue" or "topic".
	Kind string `json:"kind"`
	// Priority is the bind priority (WithQueuePriority, WithTopicPriority):
	// lower picks first under the Priority strategy. nil (omitted) when it
	// is math.MaxInt, the default: JSON clients in JS cannot hold 2^63-1.
	Priority *int `json:"priority,omitempty"`
}

// summarizeWorker is w's workerSummary.
func summarizeWorker(w Worker) workerSummary {
	s := workerSummary{
		Name:           w.Name(),
		Status:         w.Status(),
		Concurrency:    w.NumConcurrency(),
		NumProcessing:  w.NumProcessing(),
		NumPending:     w.NumPending(),
		NumIdleWorkers: w.NumIdleWorkers(),
		QueueIDs:       []string{},
	}
	if q, ok := w.(interface{ listedQueueIDs() []string }); ok {
		s.QueueIDs = q.listedQueueIDs()
	}
	return s
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
// GET /workflows serves every workflow store registered in the process, a
// producer-only process included, and PATCH /workflows/{id}/actions/cancel
// cancels in any of them. The PATCH routes change state and have no auth:
// protect Handler. The worker routes act on this process only, but PATCH /queues/{id}/actions
// pauses a shared queue for every process that consumes it, and the pause
// outlives restarts. PATCH /queues/{id}/jobs/{jobId}/actions/cancel on
// shared storage also reaches every consumer process: it marks the ID for
// 24 h (until a re-add of the ID clears it) and answers 202 for any ID with
// no ended record, known or not; a retained ended job answers 409. A waiting
// workflow task there answers 202 but still runs: cancel its workflow.
func Handler(prefix string) http.Handler {
	mux := http.StripPrefix(prefix, NewServerMux())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(InstanceHeader, InstanceID)
		mux.ServeHTTP(w, r)
	})
}

// NewServerMux returns a new [http.ServeMux] with all the REST API routes configured.
// The returned mux has no path prefix, so it can be used directly or mounted
// under any prefix using [Handler]. Its routes have no auth: see [Handler].
func NewServerMux() *http.ServeMux {
	served.Store(true) // in-memory runs are tracked for the job routes from now on
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
	// GET /queues/{id}/jobs/{jobId}
	mux.HandleFunc("GET /queues/{id}/jobs/{jobId}", handleGetJob)
	// PATCH /queues/{id}/jobs/{jobId}/actions/{action}
	mux.HandleFunc("PATCH /queues/{id}/jobs/{jobId}/actions/{action}", handleJobAction)
	// PATCH /queues/{id}/actions/{action}
	mux.HandleFunc("PATCH /queues/{id}/actions/{action}", handleQueueAction)

	// GET /workflows?state=...&offset=...&limit=...
	mux.HandleFunc("GET /workflows", handleListWorkflows)
	// GET /workflows/{id}
	mux.HandleFunc("GET /workflows/{id}", handleGetWorkflow)
	// GET /workflow-counts
	mux.HandleFunc("GET /workflow-counts", handleWorkflowCounts)
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
			workers = append(workers, summarizeWorker(worker))
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
		// Strategy is omitted for a Worker that is not varmq's: unknown.
		Strategy string         `json:"strategy,omitempty"`
		Sources  []workerSource `json:"sources"`
		Metrics  struct {
			Completed  uint64 `json:"completed"`
			Successful uint64 `json:"successful"`
			Failed     uint64 `json:"failed"`
		} `json:"metrics"`
	}{
		workerSummary: summarizeWorker(worker),
		Sources:       []workerSource{},
	}
	if p, ok := worker.(interface {
		strategyName() string
		boundSources() []workerSource
	}); ok {
		details.Strategy = p.strategyName()
		details.Sources = p.boundSources()
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
	case JobPending, JobProcessing, JobCompleted, JobFailed, JobCancelled:
	default:
		writeError(w, http.StatusBadRequest, "state must be pending, processing, completed, failed or cancelled")
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

	qi := v.(QueueInspector)
	jobs, err := qi.Jobs(r.Context(), state, offset, limit)
	if err != nil && state == JobCancelled {
		// Storage older than openapi 1.4.0 rejects the state: an empty
		// page, if it still answers a failed page.
		if _, ferr := qi.Jobs(r.Context(), JobFailed, 0, 1); ferr == nil {
			jobs, err = nil, nil
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if jobs == nil {
		jobs = []JobRecord{}
	}
	// Pages carry the size only: up to maxJobsLimit results of 64 KiB.
	for i := range jobs {
		if n := len(jobs[i].Result); n > 0 {
			jobs[i].ResultSize, jobs[i].Result = n, nil
		}
	}

	writeJSON(w, http.StatusOK, jobs)
}

// handleGetJob returns one job of a queue whose storage reads jobs by ID
// (JobReader).
func handleGetJob(w http.ResponseWriter, r *http.Request) {
	v, ok := queueRegistry.Load(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "queue not found")
		return
	}

	jr, ok := v.(JobReader)
	if !ok {
		writeError(w, http.StatusNotImplemented, "storage can't read a job by id")
		return
	}

	rec, err := jr.Job(r.Context(), r.PathValue("jobId"))
	switch {
	case errors.Is(err, ErrJobNotFound):
		writeError(w, http.StatusNotFound, "job not found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		rec.ResultSize = 0 // list pages only; a reader may fill both
		writeJSON(w, http.StatusOK, rec)
	}
}

// handleJobAction cancels a job by ID (JobCanceller). 202: a waiting job
// never runs; a running one stops only if its worker watches Context().
// Exception: a waiting workflow task on shared storage still runs; PATCH
// /workflows/{id}/actions/cancel stops it.
func handleJobAction(w http.ResponseWriter, r *http.Request) {
	v, ok := queueRegistry.Load(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "queue not found")
		return
	}

	if r.PathValue("action") != "cancel" {
		writeError(w, http.StatusBadRequest, "unknown action")
		return
	}

	jc, ok := v.(JobCanceller)
	if !ok {
		writeError(w, http.StatusNotImplemented, ErrCancelUnsupported.Error())
		return
	}

	err := jc.CancelJob(r.PathValue("jobId"))
	switch {
	case errors.Is(err, ErrJobNotFound):
		writeError(w, http.StatusNotFound, "job not found")
	case errors.Is(err, contract.ErrJobEnded), errors.Is(err, ErrJobAlreadyClosed):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		w.Header().Set(InstanceHeader, InstanceID)
		w.WriteHeader(http.StatusAccepted)
	}
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
	var page []WorkflowRecord
	for try := 0; ; try++ {
		var (
			all    []listed
			failed error
			down   []bool // per store: its listing or a read failed
			n      []int  // per store: entries listed
			ndown  int
		)
		for _, ws := range workflowStores() {
			if _, ok := ws.(WorkflowInspector); !ok {
				continue
			}
			si := len(down)
			down = append(down, false)
			got, err := listWorkflows(r, ws, si, state, want)
			n = append(n, len(got))
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

		// Walk the whole merge, not all[offset:want]: a swept entry frees its
		// slot for the next one, and a store whose read fails restarts the walk
		// without it, so the page is the other stores' merge. Read records stay
		// in all: a restart rereads only swept entries. A store that listed a
		// full want may hold unlisted entries that sort before the next one:
		// a sweep there lists again (twice at most), then the page stops at
		// that store's last listed entry, short but in order.
		page = []WorkflowRecord{}
		relist := false
		stop := len(all)
		for i, skip := 0, offset; i < stop && len(page) < limit; i++ {
			l := &all[i]
			if down[l.si] {
				continue
			}
			if skip > 0 {
				skip--
				continue
			}
			if l.rec == nil {
				rec, err := l.ws.Record(r.Context(), l.key.ID)
				if err == nil && rec.CreatedAt > l.key.CreatedAt {
					err = ErrWorkflowNotFound // ID reused since the listing: not this key's workflow
				}
				if errors.Is(err, ErrWorkflowNotFound) { // swept since the listing
					if n[l.si] < want {
						continue
					}
					if relist = try < 2; relist {
						break
					}
					for j := len(all) - 1; j >= i; j-- {
						if all[j].si == l.si {
							stop = min(stop, j+1)
							break
						}
					}
					continue
				}
				if err != nil { // the store failed since the listing
					failed, down[l.si], ndown = err, true, ndown+1
					i, skip, page, stop = -1, offset, page[:0], len(all)
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
		if !relist {
			break
		}
	}

	writeJSON(w, http.StatusOK, page)
}

// storeCounts is one entry of GET /workflow-counts. Store is a hash of the
// scope ID (the ID may hold an address or a path): equal on every instance
// that reads the same shared store, so a fleet reader counts it once.
// Counts is nil for a store that is not a WorkflowCounter.
type storeCounts struct {
	Store  string                  `json:"store,omitempty"`
	Shared bool                    `json:"shared"`
	Counts map[WorkflowState]int64 `json:"counts,omitempty"`
}

// handleWorkflowCounts returns workflows per state: one entry per durable
// store, and one for the process's in-memory stores together. A store whose
// count fails keeps its entry, with no counts (a client shows none, not a
// low total); 503 only when no store counted and one failed.
func handleWorkflowCounts(w http.ResponseWriter, r *http.Request) {
	var (
		out    = []storeCounts{}
		mem    map[WorkflowState]int64
		failed bool
		ok     bool
	)
	zero := func() map[WorkflowState]int64 {
		m := make(map[WorkflowState]int64, len(workflowStates))
		for _, st := range workflowStates {
			m[st] = 0
		}
		return m
	}

	for _, ws := range workflowStores() {
		if _, lists := ws.(WorkflowInspector); !lists {
			continue
		}
		_, inMem := ws.(*memWorkflowStore)
		scope := ws.ScopeID()
		if _, local := ws.(HostLocal); local {
			scope = hostname + "\x00" + scope // same path on two hosts: two stores
		}
		sum := sha256.Sum256([]byte(scope))
		e := storeCounts{Store: hex.EncodeToString(sum[:6]), Shared: true}

		wc, counts := ws.(WorkflowCounter)
		if !counts {
			ok = true
			out = append(out, e)
			continue
		}
		by, err := wc.WorkflowCounts(r.Context())
		if err != nil {
			failed = true // error text dropped: may hold address, keys
			if !inMem {   // ponytail: memory store never errors, no entry for it
				out = append(out, e)
			}
			continue
		}
		ok = true

		e.Counts = zero()
		if inMem {
			if mem == nil {
				mem = zero()
			}
			e.Counts = mem
		}
		for _, st := range workflowStates { // known states only
			e.Counts[st] += by[st]
		}
		if !inMem {
			out = append(out, e)
		}
	}

	if failed && !ok {
		writeError(w, http.StatusServiceUnavailable, "workflow counts unavailable")
		return
	}
	if mem != nil {
		out = append(out, storeCounts{Counts: mem})
	}
	slices.SortFunc(out, func(a, b storeCounts) int { return strings.Compare(a.Store, b.Store) })

	writeJSON(w, http.StatusOK, out)
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
