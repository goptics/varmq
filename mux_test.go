package varmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goptics/varmq/contract"
	"github.com/goptics/varmq/internal/queues"
	"github.com/goptics/varmq/mocks"
)

func setupMuxServer() (*http.ServeMux, func()) {
	WorkerRegistry.Clear()
	server := NewServerMux()
	return server, func() {
		WorkerRegistry.Range(func(key, value any) bool {
			if w, ok := value.(Worker); ok {
				w.Stop()
			}
			return true
		})
		WorkerRegistry.Clear()
	}
}

func TestMuxHealthCheck(t *testing.T) {
	server, cleanup := setupMuxServer()
	defer cleanup()

	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response map[string]string
	err = json.NewDecoder(rr.Body).Decode(&response)
	assert.NoError(t, err)
	assert.Equal(t, "ok", response["status"])
}

func TestHandler(t *testing.T) {
	WorkerRegistry.Clear()
	defer WorkerRegistry.Clear()

	handler := Handler("/varmq")
	assert.NotNil(t, handler)

	req, err := http.NewRequest("GET", "/varmq/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestMuxGetWorkerNotFound(t *testing.T) {
	server, cleanup := setupMuxServer()
	defer cleanup()

	req, err := http.NewRequest("GET", "/workers/nonexistent", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestMuxWorkerActionNotFound(t *testing.T) {
	server, cleanup := setupMuxServer()
	defer cleanup()

	req, err := http.NewRequest("PATCH", "/workers/nonexistent/actions/pause", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestMuxWorkerActionUnknown(t *testing.T) {
	WorkerRegistry.Clear()

	w := newWorker(func(j iJob[int]) {}, WithName("unknown-action-worker"))
	server := NewServerMux()
	defer w.Stop()

	req, err := http.NewRequest("PATCH", "/workers/unknown-action-worker/actions/unknown", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestMuxWorkerStartConflict(t *testing.T) {
	WorkerRegistry.Clear()

	w := newWorker(func(j iJob[int]) {}, WithName("start-conflict-worker"))
	wb := newQueues(w)
	wb.BindQueue()
	server := NewServerMux()
	defer w.Stop()

	req, err := http.NewRequest("PATCH", "/workers/start-conflict-worker/actions/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestMuxWorkerRestart(t *testing.T) {
	WorkerRegistry.Clear()

	w := newWorker(func(j iJob[int]) {}, WithName("restart-worker"), WithConcurrency(2))
	wb := newQueues(w)
	wb.BindQueue()
	server := NewServerMux()
	defer w.Stop()

	req, err := http.NewRequest("PATCH", "/workers/restart-worker/actions/restart", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestMuxWorkerRestartNotRunning(t *testing.T) {
	WorkerRegistry.Clear()

	w := newWorker(func(j iJob[int]) {}, WithName("restart-not-running-worker"), WithAutoRun(false))
	WorkerRegistry.Store("restart-not-running-worker", w)
	server := NewServerMux()
	defer w.Stop()

	req, err := http.NewRequest("PATCH", "/workers/restart-not-running-worker/actions/restart", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestMuxWorkerActionInternalError(t *testing.T) {
	WorkerRegistry.Clear()

	w := newWorker(func(j iJob[int]) {}, WithName("internal-error-worker"))
	w.StopAndWait()
	server := NewServerMux()
	defer w.Stop()

	req, err := http.NewRequest("PATCH", "/workers/internal-error-worker/actions/resume", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestMuxConcurrencyWorkerNotFound(t *testing.T) {
	server, cleanup := setupMuxServer()
	defer cleanup()

	req, err := http.NewRequest("PATCH", "/workers/nonexistent/config/concurrency/5", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestMuxConcurrencyInvalid(t *testing.T) {
	WorkerRegistry.Clear()

	w := newWorker(func(j iJob[int]) {}, WithName("concurrency-worker"))
	server := NewServerMux()
	defer w.Stop()

	req, err := http.NewRequest("PATCH", "/workers/concurrency-worker/config/concurrency/invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestMuxConcurrencySameValue(t *testing.T) {
	WorkerRegistry.Clear()

	w := newWorker(func(j iJob[int]) {}, WithName("same-concurrency-worker"), WithConcurrency(1))
	wb := newQueues(w)
	wb.BindQueue()
	server := NewServerMux()
	defer w.Stop()

	req, err := http.NewRequest("PATCH", "/workers/same-concurrency-worker/config/concurrency/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestMuxGetWorkerNilValue(t *testing.T) {
	WorkerRegistry.Clear()
	WorkerRegistry.Store("nil-worker", nil)
	server := NewServerMux()
	defer WorkerRegistry.Clear()

	req, err := http.NewRequest("GET", "/workers/nil-worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestMuxGetWorkerWrongType(t *testing.T) {
	WorkerRegistry.Clear()
	WorkerRegistry.Store("non-worker", "some string")
	server := NewServerMux()
	defer WorkerRegistry.Clear()

	req, err := http.NewRequest("GET", "/workers/non-worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestMuxListWorkers(t *testing.T) {
	// Clear registry for clean test
	WorkerRegistry.Clear()

	// Add test workers
	w1 := newWorker(func(j iJob[int]) {}, WithName("test-worker-1"), WithConcurrency(5))
	w2 := newWorker(func(j iJob[string]) {}, WithName("test-worker-2"), WithConcurrency(2))

	server := NewServerMux()

	req, err := http.NewRequest("GET", "/workers", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var workers []workerSummary
	if err := json.NewDecoder(rr.Body).Decode(&workers); err != nil {
		t.Fatal(err)
	}

	if len(workers) != 2 {
		t.Errorf("expected 2 workers, got %d", len(workers))
	}

	foundMap := make(map[string]bool)
	for _, w := range workers {
		foundMap[w.Name] = true
		if w.Name == "test-worker-1" && w.Concurrency != 5 {
			t.Errorf("expected concurrency 5 for test-worker-1, got %d", w.Concurrency)
		}
	}

	if !foundMap["test-worker-1"] || !foundMap["test-worker-2"] {
		t.Errorf("did not find expected workers in response")
	}
	for _, w := range workers {
		assert.Equal(t, []string{}, w.QueueIDs, "no queue bound: empty, not null")
	}

	w1.Stop()
	w2.Stop()
}

func TestMuxGetWorker(t *testing.T) {
	// Clear registry for clean test
	WorkerRegistry.Clear()

	w := newWorker(func(j iJob[int]) {}, WithName("detail-worker"), WithConcurrency(3))
	// Force it to start so state becomes initiated/idle
	wb := newQueues(w)
	_ = wb.BindQueue()
	assert.Eventually(t, w.IsActive, time.Second, 10*time.Millisecond)

	server := NewServerMux()

	req, err := http.NewRequest("GET", "/workers/detail-worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}

	if name, ok := response["name"].(string); !ok || name != "detail-worker" {
		t.Errorf("expected name 'detail-worker', got %v", response["name"])
	}

	if _, ok := response["metrics"]; !ok {
		t.Errorf("expected metrics payload")
	}

	// queue_ids names its in-memory queue as GET /queues lists it.
	ids, _ := response["queue_ids"].([]any)
	require.Len(t, ids, 1)
	id, _ := ids[0].(string)
	assert.True(t, strings.HasPrefix(id, "memory:"+InstanceID+":fifo:"), id)
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest("GET", "/queues/"+url.PathEscape(id)+"/jobs?state=pending", nil))
	assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	// A bound shared queue and a priority queue join it, sorted.
	shared := &inspectedQueue{MockPersistentQueue: mocks.NewMockPersistentQueue(), id: "a:shared"}
	defer queueRegistry.Delete(shared.id)
	wb.WithQueue(shared)
	pid := memOf(t, wb.BindPriorityQueue()).QueueID()
	assert.True(t, strings.HasPrefix(pid, "memory:"+InstanceID+":prio:"), pid)
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest("GET", "/workers", nil))
	var list []workerSummary
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&list))
	require.Len(t, list, 1)
	assert.Equal(t, []string{"a:shared", id, pid}, list[0].QueueIDs)

	w.Stop()
}

func TestMuxWorkerActions(t *testing.T) {
	WorkerRegistry.Clear()

	w := newWorker(func(j iJob[int]) {}, WithName("action-worker"), WithConcurrency(2))
	wb := newQueues(w)
	_ = wb.BindQueue()

	server := NewServerMux()

	// Wait briefly for worker to become active
	time.Sleep(10 * time.Millisecond)

	tests := []struct {
		action         string
		expectedStatus int
		checkState     func(*testing.T)
	}{
		{
			action:         "pause",
			expectedStatus: http.StatusOK,
			checkState: func(t *testing.T) {
				if !w.IsPaused() {
					t.Errorf("expected worker to be paused")
				}
			},
		},
		{
			action:         "resume",
			expectedStatus: http.StatusOK,
			checkState: func(t *testing.T) {
				if !w.IsActive() {
					t.Errorf("expected worker to be active")
				}
			},
		},
		{
			action:         "stop",
			expectedStatus: http.StatusOK,
			checkState: func(t *testing.T) {
				assert.Eventually(t, w.IsStopped, time.Second, 10*time.Millisecond, "expected worker to be stopped")
			},
		},
	}

	for _, tt := range tests {
		req, _ := http.NewRequest("PATCH", "/workers/action-worker/actions/"+tt.action, nil)
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, req)

		if status := rr.Code; status != tt.expectedStatus {
			t.Errorf("action %s: handler returned wrong status code: got %v want %v", tt.action, status, tt.expectedStatus)
		}

		if tt.checkState != nil {
			tt.checkState(t)
		}
	}
}

func TestMuxConcurrencyChange(t *testing.T) {
	WorkerRegistry.Clear()

	w := newWorker(func(j iJob[int]) {}, WithName("scale-worker"), WithConcurrency(2))
	wb := newQueues(w)
	_ = wb.BindQueue()

	server := NewServerMux()

	req, _ := http.NewRequest("PATCH", "/workers/scale-worker/config/concurrency/5", nil)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	if w.NumConcurrency() != 5 {
		t.Errorf("expected concurrency to be 5, got %d", w.NumConcurrency())
	}

	w.Stop()
}

// TestMuxInstanceHeader covers the instance identity header that lets clients
// tell instances apart behind a load balancer: success, error and empty-body
// responses must all carry it.
func TestMuxInstanceHeader(t *testing.T) {
	WorkerRegistry.Clear()
	defer WorkerRegistry.Clear()

	prev := InstanceID
	InstanceID = "pod-7"
	defer func() { InstanceID = prev }()

	w := newWorker(func(j iJob[int]) {}, WithName("instance-header-worker"))
	defer w.Stop()
	server := NewServerMux()

	cases := []struct{ method, path string }{
		{"GET", "/health"},
		{"GET", "/workers"},
		{"GET", "/workers/nonexistent"},
		{"PATCH", "/workers/instance-header-worker/actions/unknown"},
		{"PATCH", "/workers/instance-header-worker/actions/start"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, req)
		assert.Equal(t, "pod-7", rr.Header().Get(InstanceHeader), "%s %s", c.method, c.path)
	}

	// Responses ServeMux generates itself (unknown path, wrong method, path
	// outside the prefix) never reach writeJSON; Handler must still stamp them.
	handler := Handler("/varmq")
	for _, c := range []struct {
		method, path string
		code         int
	}{
		{"GET", "/varmq/nope", http.StatusNotFound},
		{"POST", "/varmq/workers", http.StatusMethodNotAllowed},
		{"GET", "/elsewhere", http.StatusNotFound},
	} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(c.method, c.path, nil))
		assert.Equal(t, c.code, rr.Code, "%s %s", c.method, c.path)
		assert.Equal(t, "pod-7", rr.Header().Get(InstanceHeader), "%s %s", c.method, c.path)
	}

	// Errors are JSON like every other response.
	req := httptest.NewRequest("GET", "/workers/nonexistent", nil)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)
	var body map[string]string
	assert.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	assert.Equal(t, "worker not found", body["error"])
}

// inspectedQueue is a persistent mock queue that also serves QueueInspector.
type inspectedQueue struct {
	*mocks.MockPersistentQueue
	id       string
	err      error
	gotState JobState
	gotPage  [2]int
}

func (q *inspectedQueue) QueueID() string { return q.id }

func (q *inspectedQueue) QueueStats(context.Context) (QueueStats, error) {
	return QueueStats{Counts: JobCounts{Pending: 2, Failed: 1}, Totals: JobTotals{Submitted: 5, Failed: 1}}, q.err
}

func (q *inspectedQueue) Jobs(_ context.Context, state JobState, offset, limit int) ([]JobRecord, error) {
	q.gotState, q.gotPage = state, [2]int{offset, limit}
	if q.err != nil {
		return nil, q.err
	}
	if state == JobProcessing {
		return nil, nil // storage with nothing to list
	}
	return []JobRecord{{ID: "j1", State: state, Payload: json.RawMessage(`{"n":1}`), FinishedAt: 7, Attempts: 1, Errors: []JobError{{Attempt: 1, At: 7, Error: "boom"}}}}, nil
}

func TestMuxQueues(t *testing.T) {
	queueRegistry.Clear()
	defer queueRegistry.Clear()

	// A Redis-style id needs escaping in the path; two workers bind it.
	q := &inspectedQueue{MockPersistentQueue: mocks.NewMockPersistentQueue(), id: "redisq:ns:fifo:{a/b}"}
	broken := &inspectedQueue{MockPersistentQueue: mocks.NewMockPersistentQueue(), id: "broken", err: errors.New("down")}
	for _, storage := range []IPersistentQueue{q, q, mocks.NewMockPersistentQueue(), broken} { // the plain mock is left out
		w := NewWorker(func(Job[string]) {})
		defer w.Stop()
		w.WithPersistentQueue(storage)
	}

	server := NewServerMux()
	get := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		return rr
	}

	t.Run("list", func(t *testing.T) {
		rr := get("/queues")
		require.Equal(t, http.StatusOK, rr.Code)
		assert.JSONEq(t, `[
			{"id":"broken","error":"down"},
			{"id":"redisq:ns:fifo:{a/b}",
			 "counts":{"pending":2,"processing":0,"completed":0,"failed":1},
			 "totals":{"submitted":5,"completed":0,"failed":1},"paused":false}
		]`, rr.Body.String())
	})

	t.Run("jobs", func(t *testing.T) {
		rr := get("/queues/" + url.PathEscape(q.id) + "/jobs?state=failed&offset=3&limit=2")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, JobFailed, q.gotState)
		assert.Equal(t, [2]int{3, 2}, q.gotPage)
		assert.JSONEq(t, `[{"id":"j1","state":"failed","payload":{"n":1},"finished_at":7,"attempts":1,"errors":[{"attempt":1,"at":7,"error":"boom"}]}]`, rr.Body.String())

		get("/queues/" + url.PathEscape(q.id) + "/jobs?state=pending")
		assert.Equal(t, [2]int{0, defaultJobsLimit}, q.gotPage)

		rr = get("/queues/" + url.PathEscape(q.id) + "/jobs?state=processing")
		assert.Equal(t, "[]\n", rr.Body.String(), "nil from storage is an empty array")
	})

	t.Run("errors", func(t *testing.T) {
		esc := "/queues/" + url.PathEscape(q.id) + "/jobs"
		for path, code := range map[string]int{
			"/queues/nope/jobs?state=pending":  http.StatusNotFound,
			esc:                                http.StatusBadRequest,
			esc + "?state=done":                http.StatusBadRequest,
			esc + "?state=pending&offset=-1":   http.StatusBadRequest,
			esc + "?state=pending&limit=0":     http.StatusBadRequest,
			esc + "?state=pending&limit=1001":  http.StatusBadRequest,
			esc + "?state=pending&limit=x":     http.StatusBadRequest,
			"/queues/broken/jobs?state=failed": http.StatusInternalServerError,
		} {
			rr := get(path)
			assert.Equal(t, code, rr.Code, path)
			assert.Contains(t, rr.Body.String(), `"error"`, path)
		}
	})
}

func TestWriteJSONEncodeError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, http.StatusOK, []JobRecord{{Payload: json.RawMessage(`{bad`)}})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `{"error":"encode response"}`, rr.Body.String())
}

// plainInspector is a listed queue that cannot pause.
type plainInspector struct{}

func (plainInspector) QueueID() string { return "custom:plain" }
func (plainInspector) QueueStats(context.Context) (QueueStats, error) {
	return QueueStats{}, nil
}
func (plainInspector) Jobs(context.Context, JobState, int, int) ([]JobRecord, error) {
	return nil, nil
}

// failingPauser is a listed queue whose storage cannot store the flag.
type failingPauser struct{ plainInspector }

func (failingPauser) QueueID() string { return "custom:failing" }
func (failingPauser) Pause() error    { return errors.New("redis down") }
func (failingPauser) Resume() error   { return errors.New("redis down") }
func (failingPauser) Paused() bool    { return false }

func TestMuxQueueActions(t *testing.T) {
	w := NewWorker(func(Job[int]) {}, WithName("pause-rest"))
	defer w.Stop()
	m := memOf(t, w.BindQueue())
	queueRegistry.Store("custom:plain", plainInspector{})
	queueRegistry.Store("custom:failing", failingPauser{})
	defer queueRegistry.Delete("custom:plain")
	defer queueRegistry.Delete("custom:failing")

	server := NewServerMux()
	patch := func(id, action string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest("PATCH", "/queues/"+id+"/actions/"+action, nil))
		return rr
	}

	rr := patch(m.QueueID(), "pause")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `"paused":true`)
	assert.Contains(t, rr.Body.String(), `"paused_at":`)
	s, _ := m.QueueStats(context.Background())
	assert.True(t, s.Paused)
	assert.NotZero(t, s.PausedAt)

	rr = patch(m.QueueID(), "resume")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"paused":false`)
	assert.NotContains(t, rr.Body.String(), "paused_at")

	assert.Equal(t, http.StatusBadRequest, patch(m.QueueID(), "stop").Code)
	assert.Equal(t, http.StatusNotFound, patch("nope", "pause").Code)
	assert.Equal(t, http.StatusNotImplemented, patch("custom:plain", "pause").Code)
	assert.Equal(t, http.StatusServiceUnavailable, patch("custom:failing", "resume").Code)
}

// fakeWorkflowStore is a second registered store with fixed records.
type fakeWorkflowStore struct {
	scope     string
	recs      []WorkflowRecord // newest first
	err       error
	cancelErr error
	cancelled []string // per Cancel: "nil", "true" or "false"
}

func (f *fakeWorkflowStore) ScopeID() string { return f.scope }
func (f *fakeWorkflowStore) Submit(context.Context, WorkflowPlan) error {
	return nil
}
func (f *fakeWorkflowStore) Runnable(context.Context, string, string, bool) (bool, error) {
	return true, nil
}
func (f *fakeWorkflowStore) DepResult(context.Context, string, string) ([]byte, error) {
	return nil, nil
}
func (f *fakeWorkflowStore) Record(_ context.Context, id string) (WorkflowRecord, error) {
	if f.err != nil {
		return WorkflowRecord{}, f.err
	}
	for _, r := range f.recs {
		if r.ID == id {
			return r, nil
		}
	}
	return WorkflowRecord{}, ErrWorkflowNotFound
}
func (f *fakeWorkflowStore) Cancel(_ context.Context, id string, undo *bool) error {
	s := "nil"
	if undo != nil {
		s = strconv.FormatBool(*undo)
	}
	f.cancelled = append(f.cancelled, s)
	return f.cancelErr
}
func (f *fakeWorkflowStore) Workflows(_ context.Context, state WorkflowState, offset, limit int) ([]WorkflowRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := []WorkflowRecord{}
	for _, r := range f.recs {
		if state == "" || r.State == state {
			out = append(out, r)
		}
	}
	return out[:min(len(out), limit)], nil
}

func TestMuxWorkflows(t *testing.T) {
	workflowRegistry.Clear()
	defer workflowRegistry.Clear()

	mem, clock := newMemStore(t)
	defer runtime.KeepAlive(mem) // the registry holds stores weakly
	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()
	for _, id := range []string{"m-old", "m-new"} {
		wf := NewWorkflow(mem, WithWorkflowID(id))
		a := wf.Add("a", q, 1)
		wf.Add("b", q, 2).After(a)
		require.NoError(t, submit(t, wf))
		*clock = clock.Add(2 * time.Second) // m-old 1_000_000, m-new 1_002_000
	}

	fake := &fakeWorkflowStore{scope: "fake:1", recs: []WorkflowRecord{
		{ID: "f-new", State: WorkflowFailed, CreatedAt: 1_003_000},
		{ID: "f-mid", State: WorkflowRunning, CreatedAt: 1_001_000},
		{ID: "f-old", State: WorkflowRunning, CreatedAt: 999_000},
	}}
	registerWorkflowStore(fake)

	server := NewServerMux()
	do := func(method, target string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest(method, target, nil))
		return rr
	}
	ids := func(rr *httptest.ResponseRecorder) []string {
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var recs []WorkflowRecord
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &recs))
		out := []string{}
		for _, r := range recs {
			out = append(out, r.ID)
		}
		return out
	}

	t.Run("list merges newest first", func(t *testing.T) {
		assert.Equal(t, []string{"f-new", "m-new", "f-mid", "m-old", "f-old"}, ids(do("GET", "/workflows")))
		assert.Equal(t, "application/json", do("GET", "/workflows").Header().Get("Content-Type"))
	})

	t.Run("list pages", func(t *testing.T) {
		assert.Equal(t, []string{"m-new", "f-mid"}, ids(do("GET", "/workflows?offset=1&limit=2")))
		assert.Equal(t, []string{"f-old"}, ids(do("GET", "/workflows?offset=4")))
		rr := do("GET", "/workflows?offset=99")
		assert.Equal(t, []string{}, ids(rr))
		assert.Equal(t, "[]\n", rr.Body.String())
		assert.Equal(t, []string{}, ids(do("GET", "/workflows?offset=10000&limit=1000")), "cap is inclusive")
	})

	t.Run("list filters by state", func(t *testing.T) {
		assert.Equal(t, []string{"m-new", "f-mid", "m-old", "f-old"}, ids(do("GET", "/workflows?state=running")))
		assert.Equal(t, []string{"f-new"}, ids(do("GET", "/workflows?state=failed")))
		assert.Equal(t, []string{}, ids(do("GET", "/workflows?state=cancelled")))
	})

	t.Run("list 400", func(t *testing.T) {
		for _, target := range []string{
			"/workflows?state=bogus", "/workflows?offset=-1", "/workflows?offset=x", "/workflows?offset=10001",
			"/workflows?offset=9223372036854775807",
			"/workflows?limit=0", "/workflows?limit=1001", "/workflows?limit=x",
		} {
			assert.Equal(t, http.StatusBadRequest, do("GET", target).Code, target)
		}
	})

	t.Run("get", func(t *testing.T) {
		rr := do("GET", "/workflows/m-new")
		require.Equal(t, http.StatusOK, rr.Code)
		var rec WorkflowRecord
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &rec))
		assert.Equal(t, "m-new", rec.ID)
		assert.Len(t, rec.Tasks, 2)
		assert.Contains(t, rr.Body.String(), `"created_at":1002000`)

		assert.Equal(t, "f-mid", mustRecord(t, do("GET", "/workflows/f-mid")).ID, "second store")
	})

	t.Run("get 404", func(t *testing.T) {
		rr := do("GET", "/workflows/nope")
		assert.Equal(t, http.StatusNotFound, rr.Code)
		assert.JSONEq(t, `{"error":"workflow not found"}`, rr.Body.String())
	})

	t.Run("cancel with undo", func(t *testing.T) {
		rr := do("PATCH", "/workflows/m-old/actions/cancel")
		// task a is pending in the queue and stays; waiting b is cancelled
		rec := mustRecord(t, rr)
		assert.Equal(t, JobCancelled, rec.Tasks[1].State)
		assert.Equal(t, JobCancelled, mustRecord(t, do("GET", "/workflows/m-old")).Tasks[1].State)
	})

	t.Run("cancel undo=false", func(t *testing.T) {
		rr := do("PATCH", "/workflows/f-mid/actions/cancel?undo=false")
		assert.Equal(t, "f-mid", mustRecord(t, rr).ID)
		assert.Equal(t, []string{"false"}, fake.cancelled)

		// REST may only narrow: undo=true would force undos the producer turned off.
		rr = do("PATCH", "/workflows/f-old/actions/cancel?undo=true")
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assert.JSONEq(t, `{"error":"invalid undo"}`, rr.Body.String())
		do("PATCH", "/workflows/f-old/actions/cancel")
		assert.Equal(t, []string{"false", "nil"}, fake.cancelled, "absent: the workflow's own")
	})

	t.Run("cancel 400 and 404", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, do("PATCH", "/workflows/m-new/actions/pause").Code)
		assert.Equal(t, http.StatusBadRequest, do("PATCH", "/workflows/m-new/actions/cancel?undo=maybe").Code)
		assert.Equal(t, http.StatusNotFound, do("PATCH", "/workflows/nope/actions/cancel").Code)
	})

	t.Run("cancel 503", func(t *testing.T) {
		fake.cancelErr = errors.New("redis down")
		defer func() { fake.cancelErr = nil }()
		rr := do("PATCH", "/workflows/f-new/actions/cancel")
		assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
		assert.Contains(t, rr.Body.String(), "redis down")

		fake.cancelErr = ErrWorkflowNotFound // lost the race with a purge
		assert.Equal(t, http.StatusNotFound, do("PATCH", "/workflows/f-new/actions/cancel").Code)
	})

	t.Run("409 when two stores hold the id", func(t *testing.T) {
		twin := &fakeWorkflowStore{scope: "fake:2", recs: []WorkflowRecord{{ID: "f-mid", CreatedAt: 1}}}
		registerWorkflowStore(twin)
		defer workflowRegistry.Delete("fake:2")

		n := len(fake.cancelled)
		for _, method := range []string{"GET", "PATCH"} {
			target := "/workflows/f-mid"
			if method == "PATCH" {
				target += "/actions/cancel"
			}
			rr := do(method, target)
			assert.Equal(t, http.StatusConflict, rr.Code, method)
			assert.JSONEq(t, `{"error":"workflow id in several stores"}`, rr.Body.String(), method)
		}
		assert.Len(t, fake.cancelled, n, "no store cancelled")
		assert.Empty(t, twin.cancelled)
	})

	t.Run("list skips a failing store", func(t *testing.T) {
		fake.err = errors.New("redis down")
		defer func() { fake.err = nil }()
		assert.Equal(t, []string{"m-new", "m-old"}, ids(do("GET", "/workflows")))
	})

	t.Run("503 when a store fails", func(t *testing.T) {
		fake.err = errors.New("redis down")
		defer func() { fake.err = nil }()
		assert.Equal(t, http.StatusServiceUnavailable, do("GET", "/workflows/nope").Code)
		assert.Equal(t, http.StatusServiceUnavailable, do("PATCH", "/workflows/nope/actions/cancel").Code)
		assert.Equal(t, http.StatusOK, do("GET", "/workflows/m-new").Code, "a hit wins over a failing store")

		// Not a workflow ID: 404 before any store is asked (fake would 503).
		n := len(fake.cancelled)
		for _, id := range []string{"index", "a%20b", "a:b", strings.Repeat("x", 129)} {
			assert.Equal(t, http.StatusNotFound, do("GET", "/workflows/"+id).Code, id)
			assert.Equal(t, http.StatusNotFound, do("PATCH", "/workflows/"+id+"/actions/cancel").Code, id)
		}
		assert.Len(t, fake.cancelled, n, "no store cancelled")
	})
}

// TestMuxWorkflowsAllStoresFail: 503 only when every store's listing fails.
func TestMuxWorkflowsAllStoresFail(t *testing.T) {
	workflowRegistry.Clear()
	defer workflowRegistry.Clear()

	rr := httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows", nil))
	assert.Equal(t, "[]\n", rr.Body.String(), "no store: an empty list")

	registerWorkflowStore(&fakeWorkflowStore{scope: "fake:a", err: errors.New("redis down")})
	registerWorkflowStore(&fakeWorkflowStore{scope: "fake:b", err: errors.New("redis down")})
	rr = httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Contains(t, rr.Body.String(), "redis down")
}

// keyedWorkflowStore lists keys only; its records are read one by one.
type keyedWorkflowStore struct {
	*fakeWorkflowStore
	reads  []string
	gone   string // listed, but its Record is not found
	stuck  bool   // gone stays listed (else its read sweeps it from the listing)
	swept  bool
	reused string // its Record is a newer workflow under the same ID
}

func (k *keyedWorkflowStore) WorkflowKeys(ctx context.Context, state WorkflowState, offset, limit int) ([]WorkflowKey, error) {
	recs, err := k.fakeWorkflowStore.Workflows(ctx, state, offset, limit+1)
	keys := []WorkflowKey{}
	for _, r := range recs {
		if (r.ID != k.gone || !k.swept) && len(keys) < limit {
			keys = append(keys, WorkflowKey{ID: r.ID, CreatedAt: r.CreatedAt})
		}
	}
	return keys, err
}

func (k *keyedWorkflowStore) Record(ctx context.Context, id string) (WorkflowRecord, error) {
	k.reads = append(k.reads, id)
	if id == k.gone {
		k.swept = !k.stuck
		return WorkflowRecord{}, ErrWorkflowNotFound
	}
	r, err := k.fakeWorkflowStore.Record(ctx, id)
	if id == k.reused {
		r.CreatedAt += 1000
	}
	return r, err
}

func (k *keyedWorkflowStore) Workflows(context.Context, WorkflowState, int, int) ([]WorkflowRecord, error) {
	panic("a key lister's records are not listed")
}

// TestMuxWorkflowsReadsPageOnly: keys are merged first; only the final page's
// records are read.
func TestMuxWorkflowsReadsPageOnly(t *testing.T) {
	workflowRegistry.Clear()
	defer workflowRegistry.Clear()

	k := &keyedWorkflowStore{fakeWorkflowStore: &fakeWorkflowStore{scope: "keyed", recs: []WorkflowRecord{
		{ID: "k5", CreatedAt: 5}, {ID: "k4", CreatedAt: 4}, {ID: "k3", CreatedAt: 3},
		{ID: "k2", CreatedAt: 2}, {ID: "k1", CreatedAt: 1},
	}}}
	registerWorkflowStore(k)
	registerWorkflowStore(&fakeWorkflowStore{scope: "plain", recs: []WorkflowRecord{{ID: "p", CreatedAt: 4}}})

	rr := httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows?offset=1&limit=2", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var recs []WorkflowRecord
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &recs))
	got := []string{}
	for _, r := range recs {
		got = append(got, r.ID)
	}
	assert.Equal(t, []string{"k4", "p"}, got, "CreatedAt desc, then ID")
	assert.Equal(t, []string{"k4"}, k.reads, "only the page's records are read")

	// A workflow swept between the listing and the read: k listed a full
	// page, so its next key is unknown; it lists again and k4 (ties p,
	// smaller ID) takes the slot, not p.
	k.gone = "k5"
	rr = httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows?limit=1", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &recs))
	require.Len(t, recs, 1)
	assert.Equal(t, "k4", recs[0].ID)

	// Still listed after two relists: the page stops at k's last listed
	// entry, short but in order.
	k.swept, k.stuck = false, true
	rr = httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows?limit=1", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &recs))
	assert.Empty(t, recs)

	// An ID reused between the listing and the read is not that key's
	// workflow: skipped like a swept one, so the page keeps its order.
	k.gone, k.stuck, k.swept, k.reused = "", false, false, "k5"
	rr = httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows?limit=100", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &recs))
	for _, r := range recs {
		assert.NotEqual(t, "k5", r.ID)
	}
	k.reused, k.gone, k.stuck = "", "k5", true

	// A store that listed less than a full page holds no more: its swept
	// entry is just skipped.
	rr = httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows?limit=100", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &recs))
	require.NotEmpty(t, recs)
	for _, r := range recs {
		assert.NotEqual(t, "k5", r.ID)
	}
}

// failReadStore lists keys, then fails every Record.
type failReadStore struct{ *keyedWorkflowStore }

func (failReadStore) Record(context.Context, string) (WorkflowRecord, error) {
	return WorkflowRecord{}, errors.New("redis down")
}

// TestMuxWorkflowsReadFails: a read that fails after the listing (not
// ErrWorkflowNotFound) counts as its store failing: skipped, 503 only when
// every store failed.
func TestMuxWorkflowsReadFails(t *testing.T) {
	workflowRegistry.Clear()
	defer workflowRegistry.Clear()

	get := func() *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows", nil))
		return rr
	}

	registerWorkflowStore(failReadStore{&keyedWorkflowStore{fakeWorkflowStore: &fakeWorkflowStore{
		scope: "keyed", recs: []WorkflowRecord{{ID: "k2", CreatedAt: 2}, {ID: "k1", CreatedAt: 1}}}}})
	rr := get()
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Contains(t, rr.Body.String(), "redis down")

	registerWorkflowStore(&fakeWorkflowStore{scope: "plain", recs: []WorkflowRecord{{ID: "p", CreatedAt: 3}}})
	assert.Equal(t, []string{"p"}, func() []string {
		rr := get()
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var recs []WorkflowRecord
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &recs))
		out := []string{}
		for _, r := range recs {
			out = append(out, r.ID)
		}
		return out
	}())
}

// TestMuxWorkflowsReadFailsFills: a store whose read fails leaves no short
// page: the other stores' entries fill it, past offset too.
func TestMuxWorkflowsReadFailsFills(t *testing.T) {
	workflowRegistry.Clear()
	defer workflowRegistry.Clear()

	registerWorkflowStore(failReadStore{&keyedWorkflowStore{fakeWorkflowStore: &fakeWorkflowStore{
		scope: "keyed", recs: []WorkflowRecord{{ID: "k4", CreatedAt: 4}, {ID: "k3", CreatedAt: 3}}}}})
	registerWorkflowStore(&fakeWorkflowStore{scope: "plain", recs: []WorkflowRecord{
		{ID: "p2", CreatedAt: 2}, {ID: "p1", CreatedAt: 1}}})

	for target, want := range map[string][]string{
		"/workflows?limit=1":          {"p2"},
		"/workflows?limit=2":          {"p2", "p1"},
		"/workflows?offset=1&limit=1": {"p1"},
	} {
		rr := httptest.NewRecorder()
		NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", target, nil))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var recs []WorkflowRecord
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &recs))
		got := []string{}
		for _, r := range recs {
			got = append(got, r.ID)
		}
		assert.Equal(t, want, got, target)
	}
}

// TestMuxWorkflowNoInspector: a store that cannot list still serves GET
// /workflows/{id} and cancel; GET /workflows skips it.
func TestMuxWorkflowNoInspector(t *testing.T) {
	workflowRegistry.Clear()
	defer workflowRegistry.Clear()

	fake := &fakeWorkflowStore{scope: "nolist", recs: []WorkflowRecord{{ID: "n1", CreatedAt: 1}}}
	registerWorkflowStore(plainWorkflowStore{fake}) // hides Workflows

	do := func(method, target string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		NewServerMux().ServeHTTP(rr, httptest.NewRequest(method, target, nil))
		return rr
	}
	assert.Equal(t, "n1", mustRecord(t, do("GET", "/workflows/n1")).ID)
	assert.Equal(t, "n1", mustRecord(t, do("PATCH", "/workflows/n1/actions/cancel")).ID)
	assert.Equal(t, []string{"nil"}, fake.cancelled)
	assert.Equal(t, "[]\n", do("GET", "/workflows").Body.String())
}

func mustRecord(t *testing.T, rr *httptest.ResponseRecorder) WorkflowRecord {
	t.Helper()
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var rec WorkflowRecord
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &rec))
	return rec
}

func TestWorkflowRegistryFills(t *testing.T) {
	workflowRegistry.Clear()
	defer workflowRegistry.Clear()

	a, b := NewWorkflowStore(), NewWorkflowStore()
	assert.ElementsMatch(t, []WorkflowStore{a, b}, workflowStores(),
		"two in-memory stores, one scope: both listed")

	registerWorkflowStore(nil)
	registerWorkflowStore(plainWorkflowStore{})
	_, ok := workflowRegistry.Load("plain")
	assert.True(t, ok, "a store that cannot list is registered too")
	assert.Len(t, workflowStores(), 3, "nil is not")

	assert.Equal(t, http.StatusOK, func() int {
		rr := httptest.NewRecorder()
		NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows", nil))
		return rr.Code
	}())
}

// TestWorkflowRegistryWeak: the registry does not keep a dropped in-memory
// store alive.
func TestWorkflowRegistryWeak(t *testing.T) {
	workflowRegistry.Clear()
	defer workflowRegistry.Clear()

	func() { NewWorkflowStore() }()
	runtime.GC()
	runtime.GC()
	assert.Empty(t, workflowStores())
	n := 0
	workflowRegistry.Range(func(any, any) bool { n++; return true })
	assert.Zero(t, n, "the dead entry is removed")

	kept := NewWorkflowStore()
	runtime.GC()
	assert.Len(t, workflowStores(), 1)
	runtime.KeepAlive(kept)
}

// plainWorkflowStore is a WorkflowStore that is no WorkflowInspector.
type plainWorkflowStore struct{ WorkflowStore }

func (plainWorkflowStore) ScopeID() string { return "plain" }

// inspectedCancelStore is cancelStore listed under GET /queues, its Job
// Records read from its outcomes: what a result worker left, cancelled
// ones listed under cancelled.
type inspectedCancelStore struct{ *cancelStore }

func (*inspectedCancelStore) QueueID() string { return "shared:cancel" }
func (*inspectedCancelStore) QueueStats(context.Context) (QueueStats, error) {
	return QueueStats{}, nil
}

func outcomeRecord(o JobOutcome) JobRecord {
	return JobRecord{ID: o.ID, State: o.State, Payload: json.RawMessage("null"), Attempts: 1, Result: o.Result}
}

func (s *inspectedCancelStore) Jobs(_ context.Context, state JobState, _, _ int) ([]JobRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []JobRecord
	for _, o := range s.outcomes {
		if o.State == state {
			out = append(out, outcomeRecord(o))
		}
	}
	return out, nil
}

func (s *inspectedCancelStore) Job(_ context.Context, id string) (JobRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.outcomes[id]; ok {
		return outcomeRecord(o), nil
	}
	return JobRecord{}, ErrJobNotFound
}

// brokenJobStore reads and cancels jobs, and fails at both.
type brokenJobStore struct{ plainInspector }

func (brokenJobStore) QueueID() string { return "custom:broken-jobs" }
func (brokenJobStore) Job(context.Context, string) (JobRecord, error) {
	return JobRecord{}, errors.New("redis down")
}
func (brokenJobStore) CancelJob(string) error { return errors.New("redis down") }

// sizedJobStore reads a job with both result and result_size set.
type sizedJobStore struct{ plainInspector }

func (sizedJobStore) QueueID() string { return "custom:sized" }
func (sizedJobStore) Job(context.Context, string) (JobRecord, error) {
	return JobRecord{ID: "x", State: JobCompleted, Payload: json.RawMessage("null"), Result: json.RawMessage("1"), ResultSize: 1}, nil
}

// endedJobStore cancels with an ended-job error: err, and lists one
// completed job whose result_size it fills without the result.
type endedJobStore struct {
	plainInspector
	id  string
	err error
}

func (e endedJobStore) QueueID() string        { return e.id }
func (e endedJobStore) CancelJob(string) error { return e.err }
func (endedJobStore) Jobs(context.Context, JobState, int, int) ([]JobRecord, error) {
	return []JobRecord{{ID: "x", State: JobCompleted, Payload: json.RawMessage("null"), ResultSize: 7}}, nil
}

func jobRoutes(t *testing.T) (get, cancel func(q, job string) *httptest.ResponseRecorder) {
	server := NewServerMux()
	do := func(method, path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
		assert.Contains(t, rr.Header(), InstanceHeader)
		return rr
	}
	get = func(q, job string) *httptest.ResponseRecorder {
		return do("GET", "/queues/"+url.PathEscape(q)+"/jobs/"+url.PathEscape(job))
	}
	cancel = func(q, job string) *httptest.ResponseRecorder {
		return do("PATCH", "/queues/"+url.PathEscape(q)+"/jobs/"+url.PathEscape(job)+"/actions/cancel")
	}
	return get, cancel
}

func TestMuxJobInMemory(t *testing.T) {
	started := make(chan string, 1)
	w := NewWorker(blockOnCancel(started), WithName("job-rest"))
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue()
	m := memOf(t, q)
	get, cancel := jobRoutes(t)

	run, _ := q.Add(7, WithJobId("run"))
	require.Equal(t, "run", within(t, started))
	wait, _ := q.Add(8, WithJobId("wait")) // concurrency 1: stays in the queue

	rr := get(m.QueueID(), "run")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"id":"run","state":"processing","payload":null,"attempts":1}`, rr.Body.String(), "a running job's data is not read")
	rr = get(m.QueueID(), "wait")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"id":"wait","state":"pending","payload":8,"attempts":0}`, rr.Body.String())
	assert.Equal(t, http.StatusNotFound, get(m.QueueID(), "nope").Code)
	assert.Equal(t, http.StatusNotFound, get("nope", "run").Code)

	assert.Equal(t, http.StatusNotFound, cancel(m.QueueID(), "nope").Code)
	assert.Equal(t, http.StatusNotFound, cancel("nope", "run").Code)
	rr = httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("PATCH", "/queues/"+url.PathEscape(m.QueueID())+"/jobs/run/actions/stop", nil))
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	// waiting: closed, never runs; still in the queue, so a second cancel is 409
	rr = cancel(m.QueueID(), "wait")
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	assert.Empty(t, rr.Body.String())
	assert.Equal(t, "Closed", wait.Status())
	rr = cancel(m.QueueID(), "wait")
	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.JSONEq(t, `{"error":"varmq: job already ended"}`, rr.Body.String())
	assert.Equal(t, http.StatusNotFound, get(m.QueueID(), "wait").Code, "ended jobs are not kept")

	// running: its context is cancelled
	require.Equal(t, http.StatusAccepted, cancel(m.QueueID(), "run").Code)
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	require.NoError(t, run.WaitContext(ctx), "the running job saw the cancel")
	require.NoError(t, w.WaitContext(ctx))
	assert.Equal(t, http.StatusNotFound, get(m.QueueID(), "run").Code, "ended jobs are not kept")
	assert.Equal(t, http.StatusNotFound, cancel(m.QueueID(), "run").Code)

	rr = httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/queues/"+url.PathEscape(m.QueueID())+"/jobs?state=cancelled", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "[]\n", rr.Body.String(), "in-memory queues keep no cancelled jobs")
}

func TestMuxJobSharedStorage(t *testing.T) {
	s := &inspectedCancelStore{newCancelStore()}
	started := make(chan struct{}, 1)
	w := NewResultWorker(func(j Job[int]) (int, error) {
		if j.Data() == 0 {
			started <- struct{}{}
			<-j.Context().Done()
			return 0, j.Context().Err()
		}
		return j.Data() * 2, nil
	})
	t.Cleanup(func() { w.Stop() })
	q := w.WithQueue(s) // lists s under GET /queues
	defer queueRegistry.Delete(s.QueueID())
	get, cancel := jobRoutes(t)

	done, _ := q.Add(21, WithJobId("done"))
	_, err := done.Result()
	require.NoError(t, err)
	rr := get(s.QueueID(), "done")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"id":"done","state":"completed","payload":null,"attempts":1,"result":42}`, rr.Body.String())
	rr = httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/queues/"+s.QueueID()+"/jobs?state=completed", nil))
	assert.JSONEq(t, `[{"id":"done","state":"completed","payload":null,"attempts":1,"result_size":2}]`, rr.Body.String(), "listed with its result size, not its bytes")
	assert.Equal(t, http.StatusConflict, cancel(s.QueueID(), "done").Code, "ended")
	assert.Equal(t, http.StatusNotFound, get(s.QueueID(), "nope").Code)
	assert.Equal(t, http.StatusAccepted, cancel(s.QueueID(), "nope").Code, "storage can't tell waiting from unknown")

	long, _ := q.Add(0, WithJobId("long"))
	within(t, started)
	require.Equal(t, http.StatusAccepted, cancel(s.QueueID(), "long").Code)
	_, err = long.Result()
	require.ErrorIs(t, err, ErrJobCancelled)
	rr = get(s.QueueID(), "long")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"state":"cancelled"`)

	rr = httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/queues/"+s.QueueID()+"/jobs?state=cancelled", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `[{"id":"long","state":"cancelled","payload":null,"attempts":1}]`, rr.Body.String())
}

func TestMuxJobUnsupportedAndFailing(t *testing.T) {
	queueRegistry.Store("custom:plain", plainInspector{})
	queueRegistry.Store("custom:broken-jobs", brokenJobStore{})
	defer queueRegistry.Delete("custom:plain")
	defer queueRegistry.Delete("custom:broken-jobs")
	get, cancel := jobRoutes(t)

	assert.Equal(t, http.StatusNotImplemented, get("custom:plain", "x").Code)
	assert.Equal(t, http.StatusNotImplemented, cancel("custom:plain", "x").Code)
	assert.Equal(t, http.StatusInternalServerError, get("custom:broken-jobs", "x").Code)
	rr := cancel("custom:broken-jobs", "x")
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.JSONEq(t, `{"error":"redis down"}`, rr.Body.String())

	// result_size is for list pages: a one-job read drops it.
	queueRegistry.Store("custom:sized", sizedJobStore{})
	defer queueRegistry.Delete("custom:sized")
	rr = get("custom:sized", "x")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.NotContains(t, rr.Body.String(), "result_size")

	// ended: 409 with the storage's error, plain or wrapped
	defer queueRegistry.Delete("custom:ended")
	for _, e := range []error{ErrJobAlreadyClosed, fmt.Errorf("store: %w", contract.ErrJobEnded)} {
		s := endedJobStore{id: "custom:ended", err: e}
		queueRegistry.Store(s.id, s)
		rr = cancel(s.id, "x")
		assert.Equal(t, http.StatusConflict, rr.Code)
		assert.JSONEq(t, `{"error":`+strconv.Quote(e.Error())+`}`, rr.Body.String())
	}

	// a storage-filled result_size without the result stays on the page
	rr = httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/queues/custom:ended/jobs?state=completed", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `[{"id":"x","state":"completed","payload":null,"attempts":0,"result_size":7}]`, rr.Body.String())
}

// Job IDs with path characters, escaped as one segment, reach the job
// routes under a prefix. "." and ".." can't: ServeMux cleans the path and
// redirects (307), and the Location drops the prefix.
func TestMuxJobIDEscaping(t *testing.T) {
	w := NewWorker(func(Job[int]) {}, WithName("job-rest-ids"))
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue()
	m := memOf(t, q)
	require.NoError(t, w.Pause())
	h := Handler("/admin")
	do := func(method, id, suffix string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(method, "/admin/queues/"+url.PathEscape(m.QueueID())+"/jobs/"+url.PathEscape(id)+suffix, nil))
		return rr
	}

	for _, id := range []string{"a/b", "a%b", "a b", "wf:x:y", "a//b", "a/../b", "a/./b", "/a", "a/", "a?b#c"} {
		t.Run(id, func(t *testing.T) {
			j, ok := q.Add(1, WithJobId(id))
			require.True(t, ok)
			rr := do("GET", id, "")
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			assert.Equal(t, id, mustJob(t, rr).ID)
			assert.Equal(t, http.StatusAccepted, do("PATCH", id, "/actions/cancel").Code)
			assert.True(t, j.IsClosed())
		})
	}

	enc := url.PathEscape(m.QueueID())
	for _, c := range []struct{ id, get, cancel string }{
		{".", "/queues/" + enc + "/jobs", "/queues/" + enc + "/jobs/actions/cancel"},
		{"..", "/queues/" + enc, "/queues/" + enc + "/actions/cancel"},
	} {
		q.Add(1, WithJobId(c.id))
		rr := do("GET", c.id, "")
		assert.Equal(t, http.StatusTemporaryRedirect, rr.Code, c.id)
		assert.Equal(t, c.get, rr.Header().Get("Location"), c.id)
		rr = do("PATCH", c.id, "/actions/cancel")
		assert.Equal(t, http.StatusTemporaryRedirect, rr.Code, c.id)
		assert.Equal(t, c.cancel, rr.Header().Get("Location"), c.id)
	}
}

func mustJob(t *testing.T, rr *httptest.ResponseRecorder) JobRecord {
	t.Helper()
	var r JobRecord
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &r))
	return r
}

// Two workers on one storage: a job added through the named worker's
// handle but run by the unnamed one is still read and cancelled by ID.
func TestMuxJobRunByAnotherWorker(t *testing.T) {
	shared := queues.NewQueue[any]()
	started := make(chan string, 1)
	runner := NewWorker(blockOnCancel(started)) // unnamed: lists nothing
	t.Cleanup(func() { runner.Stop() })
	runner.WithQueue(shared)
	owner := NewWorker(func(Job[int]) {}, WithName("job-rest-owner"))
	t.Cleanup(func() { owner.Stop() })
	q := owner.WithQueue(shared)
	m := memOf(t, q)
	require.NoError(t, owner.Pause()) // only runner takes jobs
	require.NoError(t, runner.Pause())
	get, cancel := jobRoutes(t)

	j, _ := q.Add(5, WithJobId("x"))
	require.NoError(t, runner.Resume()) // an add wakes only its own handle's worker
	select {
	case id := <-started:
		require.Equal(t, "x", id)
	case <-time.After(2 * time.Second):
		t.Fatal("runner never took the job")
	}
	rr := get(m.QueueID(), "x")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"id":"x","state":"processing","payload":null,"attempts":1}`, rr.Body.String())

	require.Equal(t, http.StatusAccepted, cancel(m.QueueID(), "x").Code)
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	require.NoError(t, j.WaitContext(ctx), "the runner's job saw the cancel")
	assert.Equal(t, http.StatusNotFound, get(m.QueueID(), "x").Code)
}

// A running job's data is never read: its worker writes a map while GETs
// come in (-race, and a fatal concurrent map access if it were read).
func TestMuxJobRunningPayloadNotRead(t *testing.T) {
	started := make(chan struct{})
	w := NewWorker(func(j Job[map[string]int]) {
		close(started)
		d := j.Data()
		for i := 0; j.Context().Err() == nil; i++ {
			d[strconv.Itoa(i%64)] = i
		}
	}, WithName("job-rest-map"))
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue()
	m := q.(*queue[map[string]int]).totals
	get, cancel := jobRoutes(t)

	j, _ := q.Add(map[string]int{"a": 1}, WithJobId("m"))
	within(t, started)
	for range 200 {
		rr := get(m.QueueID(), "m")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		require.JSONEq(t, `{"id":"m","state":"processing","payload":null,"attempts":1}`, rr.Body.String())
	}
	require.Equal(t, http.StatusAccepted, cancel(m.QueueID(), "m").Code)
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	require.NoError(t, j.WaitContext(ctx))
}

// A job held in a batch behind a running one is in neither the queue nor
// the run index: 404 for both routes until it starts, and it still runs.
func TestMuxJobInBatch(t *testing.T) {
	started := make(chan string, 1)
	w := NewWorker(blockOnCancel(started), WithName("job-rest-batch"), 1)
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue()
	m := memOf(t, q)
	get, cancel := jobRoutes(t)

	require.NoError(t, w.Pause())
	a, _ := q.Add(1, WithJobId("a"))
	b, _ := q.Add(2, WithJobId("b"))
	require.NoError(t, w.Resume()) // one batch: a runs, b waits behind it
	require.Equal(t, "a", within(t, started))

	assert.Equal(t, http.StatusNotFound, get(m.QueueID(), "b").Code)
	assert.Equal(t, http.StatusNotFound, cancel(m.QueueID(), "b").Code)
	assert.Equal(t, "Queued", b.Status())

	require.Equal(t, http.StatusAccepted, cancel(m.QueueID(), "a").Code)
	a.Wait()
	require.Equal(t, "b", within(t, started), "b still runs")
	assert.Equal(t, http.StatusOK, get(m.QueueID(), "b").Code)
	require.Equal(t, http.StatusAccepted, cancel(m.QueueID(), "b").Code)
	b.Wait()
}

// A job parked by a worker Pause mid-batch is in neither the queue nor
// the run index: 404 for both routes until Resume, and it still runs.
func TestMuxJobParkedByPause(t *testing.T) {
	started := make(chan string, 1)
	w := NewWorker(blockOnCancel(started), WithName("job-rest-parked"), 1)
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue()
	m := memOf(t, q)
	get, cancel := jobRoutes(t)

	require.NoError(t, w.Pause())
	a, _ := q.Add(1, WithJobId("a"))
	b, _ := q.Add(2, WithJobId("b"))
	require.NoError(t, w.Resume()) // one batch: a runs, b waits behind it
	require.Equal(t, "a", within(t, started))
	require.NoError(t, w.Pause())
	require.Equal(t, http.StatusAccepted, cancel(m.QueueID(), "a").Code)
	a.Wait()
	wk := w.(*workerBinder[int])
	require.Eventually(t, func() bool { return wk.nParked.Load() == 1 && w.Status() == "Paused" }, 2*time.Second, time.Millisecond)

	assert.Equal(t, http.StatusNotFound, get(m.QueueID(), "b").Code)
	assert.Equal(t, http.StatusNotFound, cancel(m.QueueID(), "b").Code)
	assert.Equal(t, "Queued", b.Status())

	require.NoError(t, w.Resume())
	require.Equal(t, "b", within(t, started), "b still runs")
	assert.Equal(t, http.StatusOK, get(m.QueueID(), "b").Code)
	require.Equal(t, http.StatusAccepted, cancel(m.QueueID(), "b").Code)
	b.Wait()
}

// A job added through another handle on the same storage is not this
// queue's: 404 through it, found through its own.
func TestMuxJobOtherHandleNotFound(t *testing.T) {
	shared := queues.NewQueue[any]()
	w1 := NewWorker(func(Job[int]) {}, WithName("job-rest-h1"))
	w2 := NewWorker(func(Job[int]) {}, WithName("job-rest-h2"))
	t.Cleanup(func() { w1.Stop() })
	t.Cleanup(func() { w2.Stop() })
	m1, q2 := memOf(t, w1.WithQueue(shared)), w2.WithQueue(shared)
	m2 := memOf(t, q2)
	require.NoError(t, w1.Pause())
	require.NoError(t, w2.Pause())
	get, cancel := jobRoutes(t)

	j2, _ := q2.Add(1, WithJobId("only2"))
	assert.Equal(t, http.StatusNotFound, get(m1.QueueID(), "only2").Code)
	assert.Equal(t, http.StatusOK, get(m2.QueueID(), "only2").Code)
	assert.Equal(t, http.StatusNotFound, cancel(m1.QueueID(), "only2").Code)
	assert.Equal(t, "Queued", j2.Status())
}

// Storage that rejects state=cancelled (older than openapi 1.4.0) gets an
// empty page while it still serves failed; a storage that is down is 500.
type noCancelledState struct{ plainInspector }

func (noCancelledState) QueueID() string { return "custom:no-cancelled" }
func (noCancelledState) Jobs(_ context.Context, s JobState, _, _ int) ([]JobRecord, error) {
	if s == JobCancelled {
		return nil, errors.New(`unknown job state "cancelled"`)
	}
	return nil, nil
}

func TestMuxJobsCancelledRejected(t *testing.T) {
	queueRegistry.Store("custom:no-cancelled", noCancelledState{})
	queueRegistry.Store("custom:down", &inspectedQueue{MockPersistentQueue: mocks.NewMockPersistentQueue(), id: "custom:down", err: errors.New("down")})
	defer queueRegistry.Delete("custom:no-cancelled")
	defer queueRegistry.Delete("custom:down")
	server := NewServerMux()
	list := func(id string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest("GET", "/queues/"+id+"/jobs?state=cancelled", nil))
		return rr
	}

	rr := list("custom:no-cancelled")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "[]\n", rr.Body.String())
	rr = list("custom:down")
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `{"error":"down"}`, rr.Body.String())

	// only state=cancelled falls back: a failing pending page stays 500
	queueRegistry.Store("custom:fails-pending", failsPending{})
	defer queueRegistry.Delete("custom:fails-pending")
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest("GET", "/queues/custom:fails-pending/jobs?state=pending", nil))
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// failsPending fails its pending page and serves failed.
type failsPending struct{ plainInspector }

func (failsPending) QueueID() string { return "custom:fails-pending" }
func (failsPending) Jobs(_ context.Context, s JobState, _, _ int) ([]JobRecord, error) {
	if s == JobPending {
		return nil, errors.New("pending broken")
	}
	return nil, nil
}

// hookData runs onMarshal when a REST read marshals it, then marshals M.
type hookData struct {
	M         map[string]int
	onMarshal func()
}

func (d hookData) MarshalJSON() ([]byte, error) {
	if d.onMarshal != nil {
		d.onMarshal()
	}
	return json.Marshal(d.M)
}

// A job closed during its read (its MarshalJSON closes it) is 404 on the
// one-job route and left off the pending page: the read never waits on,
// nor holds, the job's state.
func TestMuxJobCloseDuringRead(t *testing.T) {
	w := NewWorker(func(Job[hookData]) {}, WithName("job-rest-close-read"))
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue()
	m := q.(*queue[hookData]).totals
	require.NoError(t, w.Pause())
	server := NewServerMux()

	for _, path := range []string{"/jobs/c", "/jobs?state=pending"} {
		var j EnqueuedJob
		var once sync.Once
		data := hookData{M: map[string]int{"a": 1}}
		data.onMarshal = func() { once.Do(func() { require.NoError(t, j.Close()) }) }
		j, _ = q.Add(data, WithJobId("c"))

		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest("GET", "/queues/"+url.PathEscape(m.QueueID())+path, nil))
		if path == "/jobs/c" {
			assert.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		} else {
			require.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, "[]\n", rr.Body.String(), "closed during the read: not pending")
		}
		assert.True(t, j.IsClosed())
		q.Purge() // drop the closed item: the ID is free for the next round
	}
}

// within receives from ch, failing the test after 2 s instead of hanging.
func within[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting on a channel")
		var zero T
		return zero
	}
}

// A job that starts during its read (its MarshalJSON resumes the worker,
// and the start does not wait on the read) is answered running with no
// payload, and left off the pending page, the next waiting job taking its
// slot: FIFO and priority (sorted Values).
func TestMuxJobStartDuringRead(t *testing.T) {
	for _, mode := range []string{"job", "fifo", "priority"} {
		started := make(chan struct{})
		w := NewWorker(func(j Job[hookData]) {
			if j.ID() == "s" {
				close(started)
			}
			<-j.Context().Done()
		}, WithName("job-rest-start-read"), 1)
		t.Cleanup(func() { w.Stop() })
		w.BindQueue() // two sources: no batch, so only s starts and t waits in the queue
		require.NoError(t, w.Pause())
		var once sync.Once
		data := hookData{M: map[string]int{"a": 1}}
		data.onMarshal = func() {
			once.Do(func() {
				require.NoError(t, w.Resume())
				within(t, started) // the start does not wait on the read
			})
		}

		var m *memQueue
		var j, jt EnqueuedJob
		var ok bool
		next := hookData{M: map[string]int{"b": 2}}
		if mode == "priority" {
			pq := w.BindPriorityQueue()
			m = pq.(*priorityQueue[hookData]).totals
			j, ok = pq.Add(data, 0, WithJobId("s"))
			jt, _ = pq.Add(next, 1, WithJobId("t"))
		} else {
			q := w.BindQueue()
			m = q.(*queue[hookData]).totals
			j, ok = q.Add(data, WithJobId("s"))
			jt, _ = q.Add(next, WithJobId("t"))
		}
		require.True(t, ok)
		require.NotNil(t, jt)

		// list: s starts mid-read and is dropped; t fills its slot
		path, want := "/jobs/s", `{"id":"s","state":"processing","payload":null,"attempts":1}`
		if mode != "job" {
			path, want = "/jobs?state=pending&limit=1", `[{"id":"t","state":"pending","payload":{"b":2},"attempts":0}]`
		}
		rr := httptest.NewRecorder()
		NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/queues/"+url.PathEscape(m.QueueID())+path, nil))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.JSONEq(t, want, rr.Body.String(), mode)

		require.NoError(t, j.Cancel())
		ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		require.NoError(t, j.WaitContext(ctx))
		require.NoError(t, jt.Cancel())
		require.NoError(t, jt.WaitContext(ctx))
		stop()
		w.Stop()
	}
}

// Two jobs under one ID: the waiting one closed mid-read is passed over,
// and the running one answers.
func TestMuxJobCloseDuringReadFindsRun(t *testing.T) {
	started := make(chan struct{}, 1)
	w := NewWorker(func(j Job[hookData]) {
		started <- struct{}{}
		<-j.Context().Done()
	}, WithName("job-rest-two-runs"), 1)
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue()
	m := q.(*queue[hookData]).totals
	get, cancel := jobRoutes(t)

	run, _ := q.Add(hookData{M: map[string]int{"r": 1}}, WithJobId("d"))
	within(t, started)
	var second EnqueuedJob
	var once sync.Once
	data := hookData{M: map[string]int{"w": 1}}
	data.onMarshal = func() { once.Do(func() { require.NoError(t, second.Close()) }) }
	second, ok := q.Add(data, WithJobId("d")) // the first left the queue: allowed
	require.True(t, ok)

	rr := get(m.QueueID(), "d")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"id":"d","state":"processing","payload":null,"attempts":1}`, rr.Body.String())
	assert.True(t, second.IsClosed())

	require.Equal(t, http.StatusAccepted, cancel(m.QueueID(), "d").Code)
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	require.NoError(t, run.WaitContext(ctx))
}
