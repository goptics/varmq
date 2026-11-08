package varmq

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
