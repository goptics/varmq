package varmq

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

	// a Worker that is not varmq's: queue_ids [], not null
	WorkerRegistry.Store("plain", plainWorker{w1})
	defer WorkerRegistry.Delete("plain")
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest("GET", "/workers", nil))
	var raw []map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&raw))
	require.Len(t, raw, 3)
	for _, w := range raw {
		assert.JSONEq(t, `[]`, string(w["queue_ids"]))
	}
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest("GET", "/workers/plain", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var one map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&one))
	assert.JSONEq(t, `[]`, string(one["queue_ids"]))

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
	shared := &inspectedQueue{MockPersistentQueue: mocks.NewMockPersistentQueue(), id: "z:shared"}
	defer queueRegistry.Delete(shared.id)
	wb.WithQueue(shared)
	pid := memOf(t, wb.BindPriorityQueue()).QueueID()
	assert.True(t, strings.HasPrefix(pid, "memory:"+InstanceID+":prio:"), pid)
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest("GET", "/workers", nil))
	var list []workerSummary
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&list))
	require.Len(t, list, 1)
	assert.Equal(t, []string{id, pid, "z:shared"}, list[0].QueueIDs)

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
		w.WithQueue(storage)
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
func (f *fakeWorkflowStore) FinishTask(string, string, TaskEnd) ([]string, bool) {
	return nil, true
}
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
	resetWorkflowStores(t)

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
	useStore(t, fake)

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
		useStore(t, twin)
		defer contract.UnregisterWorkflowStore(twin)

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
	resetWorkflowStores(t)

	rr := httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows", nil))
	assert.Equal(t, "[]\n", rr.Body.String(), "no store: an empty list")

	useStore(t, &fakeWorkflowStore{scope: "fake:a", err: errors.New("redis down")})
	useStore(t, &fakeWorkflowStore{scope: "fake:b", err: errors.New("redis down")})
	rr = httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Contains(t, rr.Body.String(), "redis down")
}

// countingWorkflowStore also counts its workflows per state.
type countingWorkflowStore struct {
	*fakeWorkflowStore
	by map[WorkflowState]int64
}

func (c *countingWorkflowStore) WorkflowCounts(context.Context) (map[WorkflowState]int64, error) {
	return c.by, c.err
}

// localWorkflowStore is a file on this host.
type localWorkflowStore struct{ *fakeWorkflowStore }

func (localWorkflowStore) HostLocal() {}

// TestMuxWorkflowCounts: GET /workflow-counts gives one entry per durable
// store under a hashed scope, the in-memory stores summed in one, every
// state present; a failing store keeps its entry with no counts, 503 when all fail.
func TestMuxWorkflowCounts(t *testing.T) {
	resetWorkflowStores(t)

	get := func() *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflow-counts", nil))
		return rr
	}
	assert.Equal(t, "[]\n", get().Body.String(), "no store")

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()
	for _, id := range []string{"m1", "m2"} { // two in-memory stores: one entry
		mem, _ := newMemStore(t)
		defer runtime.KeepAlive(mem)
		wf := NewWorkflow(mem, WithWorkflowID(id))
		wf.Add("a", q, 1)
		require.NoError(t, submit(t, wf))
	}
	counting := &countingWorkflowStore{&fakeWorkflowStore{scope: "redis://10.0.0.5:6379/0"},
		map[WorkflowState]int64{WorkflowFailed: 3, "bogus": 9}}
	useStore(t, counting)
	useStore(t, localWorkflowStore{&fakeWorkflowStore{scope: "fake:old"}}) // lists, cannot count

	rr := get()
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	for _, part := range []string{"10.0.0.5", "6379", "redis://"} {
		assert.NotContains(t, rr.Body.String(), part, "the scope ID is hashed")
	}
	var got []storeCounts
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Len(t, got, 3)

	zero := map[WorkflowState]int64{WorkflowRunning: 0, WorkflowCompleted: 0, WorkflowFailed: 0, WorkflowCompensating: 0,
		WorkflowCompensated: 0, WorkflowCompensationFailed: 0, WorkflowCancelled: 0}
	want := func(st WorkflowState, n int64) map[WorkflowState]int64 {
		m := maps.Clone(zero)
		m[st] = n
		return m
	}
	assert.Equal(t, storeCounts{Counts: want(WorkflowRunning, 2)}, got[0], "in-memory: summed, no store id")
	var counted, old int
	for _, e := range got[1:] {
		assert.True(t, e.Shared)
		assert.Regexp(t, "^[0-9a-f]{12}$", e.Store)
		if e.Counts == nil {
			old++
		} else {
			counted++
			assert.Equal(t, want(WorkflowFailed, 3), e.Counts, "unknown states dropped")
		}
	}
	assert.Equal(t, []int{1, 1}, []int{counted, old})
	for scope, why := range map[string]string{
		counting.scope:            "same scope, same ID on every instance",
		hostname + "\x00fake:old": "host-local: the host is part of the ID",
	} {
		sum := sha256.Sum256([]byte(scope))
		assert.True(t, slices.ContainsFunc(got, func(e storeCounts) bool { return e.Store == hex.EncodeToString(sum[:6]) }), why)
	}

	counting.err = errors.New("redis down")
	rr = get()
	require.Equal(t, http.StatusOK, rr.Code)
	got = nil // Unmarshal would reuse the old maps
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Len(t, got, 3, "the failing store keeps its entry")
	sum := sha256.Sum256([]byte(counting.scope))
	i := slices.IndexFunc(got, func(e storeCounts) bool { return e.Store == hex.EncodeToString(sum[:6]) })
	require.GreaterOrEqual(t, i, 0)
	assert.True(t, got[i].Shared)
	assert.Nil(t, got[i].Counts, "failed store: entry without counts")
	assert.NotContains(t, rr.Body.String(), "redis down")

	resetWorkflowStores(t)
	useStore(t, counting)
	rr = get()
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Contains(t, rr.Body.String(), "workflow counts unavailable")
	assert.NotContains(t, rr.Body.String(), "redis down", "store error text stays out")
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
	resetWorkflowStores(t)

	k := &keyedWorkflowStore{fakeWorkflowStore: &fakeWorkflowStore{scope: "keyed", recs: []WorkflowRecord{
		{ID: "k5", CreatedAt: 5}, {ID: "k4", CreatedAt: 4}, {ID: "k3", CreatedAt: 3},
		{ID: "k2", CreatedAt: 2}, {ID: "k1", CreatedAt: 1},
	}}}
	useStore(t, k)
	useStore(t, &fakeWorkflowStore{scope: "plain", recs: []WorkflowRecord{{ID: "p", CreatedAt: 4}}})

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
	resetWorkflowStores(t)

	get := func() *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/workflows", nil))
		return rr
	}

	useStore(t, failReadStore{&keyedWorkflowStore{fakeWorkflowStore: &fakeWorkflowStore{
		scope: "keyed", recs: []WorkflowRecord{{ID: "k2", CreatedAt: 2}, {ID: "k1", CreatedAt: 1}}}}})
	rr := get()
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Contains(t, rr.Body.String(), "redis down")

	useStore(t, &fakeWorkflowStore{scope: "plain", recs: []WorkflowRecord{{ID: "p", CreatedAt: 3}}})
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
	resetWorkflowStores(t)

	useStore(t, failReadStore{&keyedWorkflowStore{fakeWorkflowStore: &fakeWorkflowStore{
		scope: "keyed", recs: []WorkflowRecord{{ID: "k4", CreatedAt: 4}, {ID: "k3", CreatedAt: 3}}}}})
	useStore(t, &fakeWorkflowStore{scope: "plain", recs: []WorkflowRecord{
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
	resetWorkflowStores(t)

	fake := &fakeWorkflowStore{scope: "nolist", recs: []WorkflowRecord{{ID: "n1", CreatedAt: 1}}}
	useStore(t, plainWorkflowStore{fake}) // hides Workflows

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
	resetWorkflowStores(t)

	a, b := NewWorkflowStore(), NewWorkflowStore()
	assert.ElementsMatch(t, []WorkflowStore{a, b}, workflowStores(),
		"two in-memory stores, one scope: both listed")

	useStore(t, plainWorkflowStore{})
	_, ok := contract.LookupWorkflowStore("plain")
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
	resetWorkflowStores(t)

	func() { NewWorkflowStore() }()
	runtime.GC()
	runtime.GC()
	assert.Empty(t, workflowStores())
	n := 0
	memWorkflows.Range(func(any, any) bool { n++; return true })
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

	for _, id := range []string{"a/b", "a%b", "a b", "w:x:y", "a//b", "a/../b", "a/./b", "/a", "a/", "a?b#c"} {
		t.Run(id, func(t *testing.T) {
			j, ok := q.Add(1, WithJobId(id))
			require.True(t, ok)
			rr := do("GET", id, "")
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			var r JobRecord
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &r))
			assert.Equal(t, id, r.ID)
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

// A job parked by a worker Pause or Stop mid-batch is in neither the
// queue nor the run index: 404 for both routes until Resume or Start, and
// it still runs.
func TestMuxJobParkedByPause(t *testing.T) {
	for _, c := range []struct {
		halt, status string
	}{{"Pause", "Paused"}, {"Stop", "Stopped"}} {
		t.Run(c.halt, func(t *testing.T) {
			started := make(chan string, 1)
			w := NewWorker(blockOnCancel(started), WithName("job-rest-parked-"+c.halt), 1, WithStoppedRetention(time.Hour))
			t.Cleanup(func() { w.Stop() })
			q := w.BindQueue()
			m := memOf(t, q)
			get, cancel := jobRoutes(t)

			require.NoError(t, w.Pause())
			a, _ := q.Add(1, WithJobId("a"))
			b, _ := q.Add(2, WithJobId("b"))
			require.NoError(t, w.Resume()) // one batch: a runs, b waits behind it
			require.Equal(t, "a", within(t, started))
			if c.halt == "Pause" {
				require.NoError(t, w.Pause())
			} else {
				require.NoError(t, w.Stop())
			}
			_ = a.Cancel()
			a.Wait()
			wk := w.(*workerBinder[int])
			require.Eventually(t, func() bool { return wk.nParked.Load() == 1 && w.Status() == c.status }, 2*time.Second, time.Millisecond)

			for _, rr := range []*httptest.ResponseRecorder{get(m.QueueID(), "b"), cancel(m.QueueID(), "b")} {
				assert.Equal(t, http.StatusNotFound, rr.Code)
				assert.JSONEq(t, `{"error":"job not found"}`, rr.Body.String(), "the queue stays listed")
			}
			assert.Equal(t, "Queued", b.Status())

			if c.halt == "Pause" {
				require.NoError(t, w.Resume())
			} else {
				require.NoError(t, w.Start())
			}
			require.Equal(t, "b", within(t, started), "b still runs")
			assert.Equal(t, http.StatusOK, get(m.QueueID(), "b").Code)
			require.Equal(t, http.StatusAccepted, cancel(m.QueueID(), "b").Code)
			b.Wait()
		})
	}
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

	// The pending page has the same scope: m1 does not list it, m2 does.
	page := func(m *memQueue) []JobRecord {
		recs, err := m.Jobs(context.Background(), JobPending, 0, 10)
		require.NoError(t, err)
		return recs
	}
	assert.Empty(t, page(m1))
	require.Len(t, page(m2), 1)
	assert.Equal(t, "only2", page(m2)[0].ID)
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

// WithEndedJobs over REST: an ended result job reads with its result,
// pages carry result_size, cancel is 409; a queue without it keeps none.
func TestMuxJobEndedKept(t *testing.T) {
	w := NewResultWorker(func(j Job[int]) (string, error) {
		if j.Data() < 0 {
			return "", errors.New("boom")
		}
		return "done", nil
	}, WithName("job-rest-ended"))
	t.Cleanup(func() { w.Stop() })
	q := w.BindQueue(WithEndedJobs(2))
	off := w.BindQueue()
	get, cancel := jobRoutes(t)
	id := q.(*resultQueue[int, string]).totals.QueueID()
	offID := off.(*resultQueue[int, string]).totals.QueueID()
	page := func(qid, state string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		NewServerMux().ServeHTTP(rr, httptest.NewRequest("GET", "/queues/"+url.PathEscape(qid)+"/jobs?state="+state, nil))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		return rr
	}

	for _, h := range []ResultQueue[int, string]{q, off} {
		ok, _ := h.Add(1, WithJobId("ok"))
		_, err := ok.Result()
		require.NoError(t, err)
		bad, _ := h.Add(-1, WithJobId("bad"))
		_, err = bad.Result()
		require.Error(t, err)
	}

	rr := get(id, "ok")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var rec JobRecord
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &rec))
	assert.JSONEq(t, `"done"`, string(rec.Result))
	assert.Zero(t, rec.ResultSize, "one-job read: result, no size")
	assert.Equal(t, http.StatusConflict, cancel(id, "ok").Code)
	assert.Equal(t, http.StatusConflict, cancel(id, "bad").Code)

	var recs []JobRecord
	require.NoError(t, json.Unmarshal(page(id, "completed").Body.Bytes(), &recs))
	require.Len(t, recs, 1)
	assert.Equal(t, len(`"done"`), recs[0].ResultSize)
	assert.Nil(t, recs[0].Result, "pages: size only")
	require.NoError(t, json.Unmarshal(page(id, "failed").Body.Bytes(), &recs))
	require.Len(t, recs, 1)
	assert.Equal(t, "bad", recs[0].ID)
	assert.Equal(t, "boom", recs[0].Errors[0].Error)

	for _, st := range []string{"completed", "failed", "cancelled"} {
		assert.Equal(t, "[]\n", page(offID, st).Body.String(), "off by default: %s", st)
	}
	assert.Equal(t, http.StatusNotFound, get(offID, "ok").Code)
	assert.Equal(t, http.StatusNotFound, cancel(offID, "ok").Code)
}

// getWorkerRaw is GET /workers/{name} decoded field by field.
func getWorkerRaw(t *testing.T, server http.Handler, name string) map[string]json.RawMessage {
	t.Helper()
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest("GET", "/workers/"+name, nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var got map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	return got
}

// GET /workers/{name} sends the worker's strategy and its sources in pick
// order (priority, ties in bind order); GET /workers does not.
func TestMuxGetWorkerSources(t *testing.T) {
	WorkerRegistry.Clear()
	defer WorkerRegistry.Clear()
	server := NewServerMux()

	// sources reads name's sources and checks each non-empty queue_id is
	// one of its queue_ids.
	sources := func(t *testing.T, name string) []workerSource {
		t.Helper()
		raw := getWorkerRaw(t, server, name)
		var got []workerSource
		var ids []string
		require.NoError(t, json.Unmarshal(raw["sources"], &got))
		require.NoError(t, json.Unmarshal(raw["queue_ids"], &ids))
		for _, src := range got {
			if src.QueueID != "" {
				assert.Contains(t, ids, src.QueueID, "sources[].queue_id is in queue_ids")
			}
		}
		return got
	}

	t.Run("no source", func(t *testing.T) {
		w := newWorker(func(j iJob[int]) {}, WithName("src-none"))
		defer w.Stop()
		got := getWorkerRaw(t, server, "src-none")
		assert.JSONEq(t, `"priority"`, string(got["strategy"]), "default strategy")
		assert.JSONEq(t, `[]`, string(got["sources"]), "empty, not null")
		assert.Empty(t, sources(t, "src-none"))
	})

	t.Run("one in-memory queue", func(t *testing.T) {
		w := newWorker(func(j iJob[int]) {}, WithName("src-one"))
		defer w.Stop()
		id := memOf(t, newQueues(w).BindQueue()).QueueID()
		assert.Equal(t, []workerSource{{QueueID: id, Kind: "queue"}}, sources(t, "src-one"))
		assert.JSONEq(t, `[{"queue_id":"`+id+`","kind":"queue"}]`, string(getWorkerRaw(t, server, "src-one")["sources"]), "default priority (math.MaxInt): omitted")
	})

	t.Run("priorities in priority order", func(t *testing.T) {
		w := newWorker(func(j iJob[int]) {}, WithName("src-prio"), WithStrategy(Priority))
		defer w.Stop()
		wb := newQueues(w)
		low := memOf(t, wb.BindQueue(WithQueuePriority(5))).QueueID()
		shared := &inspectedQueue{MockPersistentQueue: mocks.NewMockPersistentQueue(), id: "z:src-prio"}
		defer queueRegistry.Delete(shared.id)
		wb.WithQueue(shared, WithQueuePriority(1))
		anon := mocks.NewMockPersistentQueue() // acks, no QueueInspector: no ID
		wb.WithQueue(anon, WithQueuePriority(3))

		assert.JSONEq(t, `"priority"`, string(getWorkerRaw(t, server, "src-prio")["strategy"]))
		src := sources(t, "src-prio")
		assert.Equal(t, []workerSource{
			{QueueID: "z:src-prio", Kind: "queue", Priority: new(1)},
			{QueueID: "", Kind: "queue", Priority: new(3)},
			{QueueID: low, Kind: "queue", Priority: new(5)},
		}, src, "priority order, not bind order")
	})

	t.Run("two handles on one storage", func(t *testing.T) {
		w := newWorker(func(j iJob[int]) {}, WithName("src-twice"))
		defer w.Stop()
		wb := newQueues(w)
		q := queues.NewQueue[any]()
		var handleIDs []string
		for range 7 {
			handleIDs = append(handleIDs, memOf(t, wb.WithQueue(q)).QueueID())
		}
		first := slices.Min(handleIDs)
		other := memOf(t, wb.BindQueue()).QueueID()
		want := []workerSource{}
		for range handleIDs {
			want = append(want, workerSource{QueueID: first, Kind: "queue"})
		}
		want = append(want, workerSource{QueueID: other, Kind: "queue"})
		for range 20 { // sync.Map order varies: the smallest wins every time
			require.Equal(t, want, sources(t, "src-twice"), "the smallest ID of the storage's handles")
		}
	})

	t.Run("explicit priorities", func(t *testing.T) {
		w := newWorker(func(j iJob[int]) {}, WithName("src-explicit"))
		defer w.Stop()
		wb := newQueues(w)
		maxID := memOf(t, wb.BindQueue(WithQueuePriority(math.MaxInt))).QueueID()
		zero := memOf(t, wb.BindQueue(WithQueuePriority(0))).QueueID()
		neg := memOf(t, wb.BindQueue(WithQueuePriority(-1))).QueueID()
		assert.Equal(t, []workerSource{
			{QueueID: neg, Kind: "queue", Priority: new(-1)},
			{QueueID: zero, Kind: "queue", Priority: new(0)},
			{QueueID: maxID, Kind: "queue"},
		}, sources(t, "src-explicit"), "-1, 0, then math.MaxInt (omitted, as unset)")
	})

	t.Run("priority queue", func(t *testing.T) {
		w := newWorker(func(j iJob[int]) {}, WithName("src-pq"))
		defer w.Stop()
		wb := newQueues(w)
		fifo := memOf(t, wb.BindQueue()).QueueID()
		pq := memOf(t, wb.BindPriorityQueue(WithQueuePriority(2))).QueueID()
		assert.True(t, strings.HasPrefix(pq, "memory:"+InstanceID+":prio:"), pq)
		assert.Equal(t, []workerSource{
			{QueueID: pq, Kind: "queue", Priority: new(2)},
			{QueueID: fifo, Kind: "queue"},
		}, sources(t, "src-pq"))
	})

	t.Run("storage with a map in an interface field", func(t *testing.T) {
		w := newWorker(func(j iJob[int]) {}, WithName("src-valq"))
		defer w.Stop()
		newQueues(w).WithQueue(valQ{IQueue: queues.NewQueue[any](), tag: map[string]int{}})
		assert.Equal(t, []workerSource{{QueueID: "", Kind: "queue"}}, sources(t, "src-valq"), "no panic; not matched to its memQueue")
	})

	t.Run("topic, then unsubscribed", func(t *testing.T) {
		w := newWorker(func(j iJob[int]) {}, WithName("src-topic"), WithStrategy(RoundRobin))
		defer w.Stop()
		wb := newQueues(w)
		id := memOf(t, wb.BindQueue(WithQueuePriority(2))).QueueID()
		sub := wb.WithTopic(NewTopic[int](), WithTopicPriority(1))

		assert.JSONEq(t, `"round_robin"`, string(getWorkerRaw(t, server, "src-topic")["strategy"]))
		assert.Equal(t, []workerSource{
			{QueueID: "", Kind: "topic", Priority: new(1)},
			{QueueID: id, Kind: "queue", Priority: new(2)},
		}, sources(t, "src-topic"))

		sub.Unsubscribe()
		assert.Equal(t, []workerSource{{QueueID: id, Kind: "queue", Priority: new(2)}}, sources(t, "src-topic"))
	})

	t.Run("strategy names", func(t *testing.T) {
		for s, want := range map[Strategy]string{RoundRobin: "round_robin", MaxLen: "max_len", MinLen: "min_len", Priority: "priority", Strategy(9): "priority"} {
			w := newWorker(func(j iJob[int]) {}, WithName("src-strategy"), WithStrategy(s))
			assert.JSONEq(t, strconv.Quote(want), string(getWorkerRaw(t, server, "src-strategy")["strategy"]), "strategy %d", s)
			w.Stop()
		}
	})

	t.Run("summary has neither", func(t *testing.T) {
		w := newWorker(func(j iJob[int]) {}, WithName("src-summary"))
		defer w.Stop()
		newQueues(w).BindQueue()
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest("GET", "/workers", nil))
		var list []map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&list))
		for _, one := range list {
			assert.NotContains(t, one, "strategy")
			assert.NotContains(t, one, "sources")
		}
	})

	t.Run("not varmq's worker", func(t *testing.T) {
		w := newWorker(func(j iJob[int]) {}, WithName("src-inner"))
		defer w.Stop()
		WorkerRegistry.Store("src-plain", plainWorker{w})
		defer WorkerRegistry.Delete("src-plain")
		got := getWorkerRaw(t, server, "src-plain")
		assert.NotContains(t, got, "strategy", "unknown: omitted")
		assert.JSONEq(t, `[]`, string(got["sources"]))
	})
}

// valQ is storage of a comparable type whose value == panics on: a map in
// an interface field.
type valQ struct {
	IQueue
	tag any
}

// uncomparableQueue is storage whose type == panics on.
type uncomparableQueue struct{ m map[int]int }

func (uncomparableQueue) Len() int             { return 0 }
func (uncomparableQueue) Dequeue() (any, bool) { return nil, false }
func (uncomparableQueue) Values() []any        { return nil }
func (uncomparableQueue) Purge()               {}
func (uncomparableQueue) Close() error         { return nil }

func TestSameStorage(t *testing.T) {
	a, b := queues.NewQueue[any](), queues.NewQueue[any]()
	assert.True(t, sameStorage(a, a))
	assert.False(t, sameStorage(a, b))
	assert.False(t, sameStorage(a, queues.NewPriorityQueue[any]()), "other type")
	u := uncomparableQueue{m: map[int]int{}}
	assert.NotPanics(t, func() { assert.False(t, sameStorage(u, u)) })
	v := valQ{IQueue: a, tag: map[string]int{}}
	assert.NotPanics(t, func() { assert.False(t, sameStorage(v, v)) }, "map in an interface field")
	c := valQ{IQueue: a, tag: 1}
	assert.True(t, sameStorage(c, c), "comparable value")
}

// resetWorkflowStores empties both workflow registries now and after the
// test.
func resetWorkflowStores(t *testing.T) {
	t.Helper()
	reset := func() {
		for _, s := range contract.WorkflowStores() {
			contract.UnregisterWorkflowStore(s)
		}
		memWorkflows.Clear()
	}
	reset()
	t.Cleanup(reset)
}

// withHubTiming lowers the protocol floors (1 s heartbeat, 0.5–30 s
// backoff) so hub tests run in milliseconds.
func withHubTiming(minBeat, firstRetry, maxRetry time.Duration) HubOption {
	return func(c *hubConfig) { c.minBeat, c.firstRetry, c.maxRetry = minBeat, firstRetry, maxRetry }
}

// fakeHub is a hub (spec/hub.md) for tests: every stream it opens arrives
// on streams, every reply on replies, every beat on beats. Each stream gets
// a new session, put in place of $SESSION in hello.
type fakeHub struct {
	srv          *httptest.Server
	token        string
	hello        atomic.Value // string: first stream line, "" for none
	streamStatus atomic.Int32 // non-zero: refuse streams with it
	beatStatus   atomic.Int32 // non-zero: answer beats with it
	replyStatus  atomic.Int32 // non-zero: answer replies with it
	replyHang    atomic.Bool  // hold replies until the caller gives up
	streams      chan *hubStream
	replies      chan hubReply
	beats        chan hubBeat
}

// hubStream is one open stream: send lines down it, close quit to end it.
type hubStream struct {
	query   url.Values
	session string // this stream's secret, sent in hello
	lines   chan string
	quit    chan struct{}
}

// hubBeat is one beat: its query and its X-Hub-Session header.
type hubBeat struct {
	url.Values
	session string
}

func (s *hubStream) send(lines ...string) {
	for _, l := range lines {
		s.lines <- l
	}
}

// hello sends a hello of heartbeat ms with this stream's session.
func (s *hubStream) hello(ms int) {
	s.send(strings.ReplaceAll(hubHello(ms), "$SESSION", s.session))
}

// request sends a request frame.
func (s *hubStream) request(id, method, path string) {
	b, _ := json.Marshal(map[string]string{"type": "request", "id": id, "method": method, "path": path})
	s.send(string(b))
}

type hubReply struct {
	id, instance           string
	status, ctype, varmqID string
	session, url           string // X-Hub-Session; path and query
	body                   string
}

// hubHello is a hello whose session the fake hub fills in per stream.
func hubHello(ms int) string {
	return fmt.Sprintf(`{"type":"hello","protocol":1,"heartbeat_ms":%d,"session":"$SESSION"}`, ms)
}

// hubSession is a new session secret: 26 bytes of [A-Z2-7].
func hubSession() string { return rand.Text() }

func newFakeHub(t *testing.T) *fakeHub {
	t.Helper()
	f := &fakeHub{
		token:   "s3cret",
		streams: make(chan *hubStream, 16),
		replies: make(chan hubReply, 64),
		beats:   make(chan hubBeat, 256),
	}
	f.hello.Store(hubHello(5000))
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /hub/stream", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		if code := f.streamStatus.Load(); code != 0 {
			w.WriteHeader(int(code))
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		fl := w.(http.Flusher)
		write := func(l string) { io.WriteString(w, l+"\n"); fl.Flush() }
		s := &hubStream{query: r.URL.Query(), session: hubSession(), lines: make(chan string, 64), quit: make(chan struct{})}
		if h := f.hello.Load().(string); h != "" {
			write(strings.ReplaceAll(h, "$SESSION", s.session))
		} else {
			fl.Flush()
		}
		select { // undrained (redial tests): never block srv.Close
		case f.streams <- s:
		case <-r.Context().Done():
			return
		}
		for {
			select {
			case l := <-s.lines:
				write(l)
			case <-s.quit:
				return
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("POST /hub/reply/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		body, _ := io.ReadAll(r.Body) // read first: a closed client then ends r.Context()
		if f.replyHang.Load() {
			<-r.Context().Done()
			return
		}
		select {
		case f.replies <- hubReply{
			id: r.PathValue("id"), instance: r.URL.Query().Get("instance"),
			status: r.Header.Get("X-Hub-Status"), ctype: r.Header.Get("Content-Type"),
			varmqID: r.Header.Get(InstanceHeader), session: r.Header.Get("X-Hub-Session"),
			url: r.URL.RequestURI(), body: string(body),
		}:
		case <-r.Context().Done():
		}
		code := int(f.replyStatus.Load())
		if code == 0 {
			code = http.StatusNoContent
		}
		w.WriteHeader(code)
	})
	mux.HandleFunc("POST /hub/beat", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		select {
		case f.beats <- hubBeat{r.URL.Query(), r.Header.Get("X-Hub-Session")}:
		default:
		}
		code := int(f.beatStatus.Load())
		if code == 0 {
			code = http.StatusNoContent
		}
		w.WriteHeader(code)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close) // after connect's cleanup (LIFO): ConnectHub has returned

	old := InstanceID
	InstanceID = "hub-test"
	t.Cleanup(func() { InstanceID = old })
	return f
}

// connect runs ConnectHub against f until stop or the test ends. Errors
// reach errs (dropped once it is full).
func (f *fakeHub) connect(t *testing.T, opts ...HubOption) (errs chan error, stop func() error) {
	t.Helper()
	errs = make(chan error, 256)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	opts = append([]HubOption{
		withHubTiming(10*time.Millisecond, 10*time.Millisecond, 40*time.Millisecond),
		WithHubErrHandler(func(err error) {
			select {
			case errs <- err:
			default:
			}
		}),
	}, opts...)
	runners := new(sync.WaitGroup)
	opts = append(opts, func(c *hubConfig) { c.runners = runners })
	go func() { done <- ConnectHub(ctx, f.srv.URL, f.token, opts...) }()
	var (
		once sync.Once
		err  error
	)
	stop = func() error {
		once.Do(func() { cancel(); err = within(t, done) })
		return err
	}
	t.Cleanup(func() {
		stop()
		// a handler that outlived ConnectHub reads InstanceID: wait for
		// it before newFakeHub's cleanup restores InstanceID
		idle := make(chan struct{})
		go func() { runners.Wait(); close(idle) }()
		within(t, idle)
	})
	return errs, stop
}

// reply waits for the reply to request id.
func (f *fakeHub) reply(t *testing.T, id string) hubReply {
	t.Helper()
	r := within(t, f.replies)
	require.Equal(t, id, r.id)
	return r
}

// errIs waits until errs carries an error matching ok, skipping others.
func errIs(t *testing.T, errs chan error, ok func(error) bool) error {
	t.Helper()
	deadline := time.After(5 * time.Second) // redials keep errors coming
	for {
		select {
		case err := <-errs:
			if ok(err) {
				return err
			}
		case <-deadline:
			t.Fatal("timed out waiting for a matching error")
			return nil
		}
	}
}

// hubStatus matches a hubStatusError of op and code.
func hubStatus(op string, code int) func(error) bool {
	return func(err error) bool {
		var se *hubStatusError
		return errors.As(err, &se) && se.op == op && se.code == code
	}
}

func TestHubConnectGetAndPatch(t *testing.T) {
	WorkerRegistry.Clear()
	f := newFakeHub(t)
	w := NewWorker(func(Job[int]) {}, WithName("hub-worker"))
	t.Cleanup(func() { w.Stop(); WorkerRegistry.Clear() })
	w.BindQueue()

	f.connect(t)
	s := within(t, f.streams)
	assert.Equal(t, "hub-test", s.query.Get("instance"))
	assert.Equal(t, "1", s.query.Get("protocol"))

	s.request("g1", "GET", "/workers/hub-worker")
	r := f.reply(t, "g1")
	assert.Equal(t, "200", r.status)
	assert.Equal(t, s.session, r.session)
	assert.Equal(t, "application/json", r.ctype)
	assert.Equal(t, "hub-test", r.varmqID)
	assert.Equal(t, "hub-test", r.instance)
	var got workerSummary
	require.NoError(t, json.Unmarshal([]byte(r.body), &got))
	assert.Equal(t, "hub-worker", got.Name)

	s.request("p1", "PATCH", "/workers/hub-worker/actions/pause")
	r = f.reply(t, "p1")
	assert.Equal(t, "200", r.status)
	assert.Empty(t, r.body)
	assert.Empty(t, r.ctype, "no body, no type")
	assert.Equal(t, "hub-test", r.varmqID)
	assert.True(t, w.IsPaused())

	s.request("n1", "GET", "/nope?x=1")
	r = f.reply(t, "n1")
	assert.Equal(t, "404", r.status)
	assert.Equal(t, "hub-test", r.varmqID)
}

func TestHubReadOnly(t *testing.T) {
	WorkerRegistry.Clear()
	f := newFakeHub(t)
	w := NewWorker(func(Job[int]) {}, WithName("hub-ro"))
	t.Cleanup(func() { w.Stop(); WorkerRegistry.Clear() })
	w.BindQueue()

	f.connect(t, WithHubReadOnly())
	s := within(t, f.streams)
	s.request("p", "PATCH", "/workers/hub-ro/actions/pause")
	r := f.reply(t, "p")
	assert.Equal(t, "403", r.status)
	assert.Equal(t, "application/json", r.ctype)
	assert.JSONEq(t, `{"error":"read-only hub connection"}`, r.body)
	assert.False(t, w.IsPaused(), "the handler never ran")

	s.request("g", "GET", "/health")
	assert.Equal(t, "200", f.reply(t, "g").status)
}

// hubJobStore serves one job with a payload and a result.
type hubJobStore struct{ id string }

func (s hubJobStore) QueueID() string { return s.id }
func (hubJobStore) QueueStats(context.Context) (QueueStats, error) {
	return QueueStats{}, nil
}
func (hubJobStore) Jobs(context.Context, JobState, int, int) ([]JobRecord, error) {
	return []JobRecord{{ID: "j", State: JobCompleted, Payload: json.RawMessage(`{"secret":1}`), Result: json.RawMessage(`{"secret":2}`)}}, nil
}
func (hubJobStore) Job(context.Context, string) (JobRecord, error) {
	return JobRecord{ID: "j", State: JobCompleted, Payload: json.RawMessage(`{"secret":1}`), Result: json.RawMessage(`{"secret":2}`)}, nil
}

func TestHubNoPayloads(t *testing.T) {
	id := "custom:hub/jobs" // escaped in the path
	queueRegistry.Store(id, hubJobStore{id})
	t.Cleanup(func() { queueRegistry.Delete(id) })
	f := newFakeHub(t)
	f.connect(t, WithHubNoPayloads())
	s := within(t, f.streams)

	base := "/queues/" + url.PathEscape(id) + "/jobs"
	s.request("list", "GET", base+"?state=completed")
	r := f.reply(t, "list")
	require.Equal(t, "200", r.status, r.body)
	assert.JSONEq(t, `[{"id":"j","state":"completed","payload":null,"attempts":0,"result_size":12}]`, r.body)

	s.request("one", "GET", base+"/j")
	r = f.reply(t, "one")
	require.Equal(t, "200", r.status, r.body)
	assert.JSONEq(t, `{"id":"j","state":"completed","payload":null,"attempts":0}`, r.body)
	assert.Equal(t, "application/json", r.ctype)

	s.request("q", "GET", "/queues")
	r = f.reply(t, "q")
	assert.Equal(t, "200", r.status)
	assert.NotContains(t, r.body, "secret")
}

// An unknown message type and unknown fields are ignored; a bad frame
// with an id is answered 400, one without a valid id is dropped.
func TestHubFrames(t *testing.T) {
	f := newFakeHub(t)
	errs, _ := f.connect(t)
	s := within(t, f.streams)

	s.send(`{"type":"config","report_interval_ms":5000}`, `{"type":"ping"}`, `not json`)
	s.send(`{"type":"request","id":"u","method":"GET","path":"/health","extra":true}`)
	r := f.reply(t, "u")
	assert.Equal(t, "200", r.status)
	assert.JSONEq(t, `{"status":"ok"}`, r.body)

	s.request("bad id!", "GET", "/health")
	s.request("m", "DELETE", "/health")
	s.request("p", "GET", "health")
	got := map[string]hubReply{}
	for range 2 {
		r := within(t, f.replies)
		got[r.id] = r
	}
	for _, id := range []string{"m", "p"} {
		assert.Equal(t, "400", got[id].status, id)
		assert.Equal(t, "application/json", got[id].ctype)
		assert.Contains(t, got[id].body, `"error"`)
	}
	errIs(t, errs, func(err error) bool { return strings.Contains(err.Error(), "bad request id") })

	select {
	case <-f.streams:
		t.Fatal("redialed on an unknown type")
	default:
	}
}

func TestHubUnauthorizedRetries(t *testing.T) {
	f := newFakeHub(t)
	f.streamStatus.Store(http.StatusUnauthorized)
	errs, _ := f.connect(t)

	err := errIs(t, errs, func(err error) bool {
		var se *hubStatusError
		return errors.As(err, &se) && se.code == http.StatusUnauthorized
	})
	assert.Contains(t, err.Error(), "401")
	f.streamStatus.Store(0) // the token is fixed: the next retry heals
	within(t, f.streams)
}

func TestHubStreamDropRedials(t *testing.T) {
	f := newFakeHub(t)
	errs, _ := f.connect(t)
	s := within(t, f.streams)
	close(s.quit)
	errIs(t, errs, func(err error) bool { return errors.Is(err, errHubClosed) })
	s = within(t, f.streams)
	s.request("after", "GET", "/health")
	assert.Equal(t, "200", f.reply(t, "after").status)
}

func TestHubStaleRedials(t *testing.T) {
	f := newFakeHub(t)
	f.hello.Store(hubHello(20)) // no pings follow
	errs, _ := f.connect(t)
	within(t, f.streams)
	errIs(t, errs, func(err error) bool { return errors.Is(err, errHubStale) })
	within(t, f.streams)
	b := within(t, f.beats) // beats ran meanwhile
	assert.Equal(t, "hub-test", b.Get("instance"))
	assert.Empty(t, b.Get("bye"))
}

func TestHubBeatNotFoundRedials(t *testing.T) {
	f := newFakeHub(t)
	f.hello.Store(hubHello(20))
	f.beatStatus.Store(http.StatusNotFound)
	errs, _ := f.connect(t)
	errIs(t, errs, func(err error) bool { return errors.Is(err, errHubNoStream) })
}

func TestHubByeOnCancel(t *testing.T) {
	f := newFakeHub(t)
	_, stop := f.connect(t)
	s := within(t, f.streams)
	s.request("h", "GET", "/health") // its reply: hello was read
	f.reply(t, "h")
	assert.ErrorIs(t, stop(), context.Canceled)
	for {
		if b := within(t, f.beats); b.Get("bye") == "1" {
			assert.Equal(t, "hub-test", b.Get("instance"))
			assert.Equal(t, s.session, b.session)
			return
		}
	}
}

func TestHubConfigErrors(t *testing.T) {
	old := InstanceID
	t.Cleanup(func() { InstanceID = old })
	InstanceID = "hub-test"
	ctx := context.Background()

	for _, u := range []string{"http://example.com", "http://10.0.0.1:8080", "ftp://localhost", "localhost:80", "https://h/?q=1", "://"} {
		assert.Error(t, ConnectHub(ctx, u, "t"), u)
	}
	err := ConnectHub(ctx, "http://example.com", "t")
	assert.ErrorContains(t, err, "WithHubInsecure")
	assert.Error(t, ConnectHub(ctx, "https://h", ""))
	assert.Error(t, ConnectHub(ctx, "https://h", "a\nb"))

	InstanceID = ""
	assert.ErrorContains(t, ConnectHub(ctx, "https://h", "t"), "InstanceID")
	// Revision A: UTF-8, no byte below 0x20, no 0x7F, 1 to 128 bytes; the
	// error is fixed and never echoes the ID
	for _, id := range []string{strings.Repeat("x", 129), "a\x01b", "tab\there", "a\nb", "del\x7f", "bad\xffutf8", "\xc3"} {
		InstanceID = id
		err := ConnectHub(ctx, "https://h", "t")
		assert.ErrorIs(t, err, errHubBadID, "%q", id)
		assert.ErrorContains(t, err, "InstanceID")
		assert.False(t, strings.ContainsFunc(err.Error(), func(r rune) bool { return r < 0x20 || r == 0x7f }), "%q", id)
	}
	cctx0, cancel0 := context.WithCancel(ctx)
	cancel0()
	for _, id := range []string{strings.Repeat("x", 128), "héllo wörld-ü", "a"} {
		InstanceID = id
		assert.ErrorIs(t, ConnectHub(cctx0, "https://h", "t"), context.Canceled, "%q", id)
	}
	InstanceID = "hub-test"

	for _, h := range []string{"localhost", "127.0.0.1", "::1"} {
		assert.True(t, loopback(h), h)
	}
	assert.False(t, loopback("example.com"))

	// userinfo is refused, and no config error repeats credentials
	for _, u := range []string{
		"https://user:pw123@hub.example", "http://user:pw123@localhost:1", "https://user:pw123@hub.example?x=1",
		"https://user:pw123@hub.example/%zz", "http://user:pw123@example.com", "https://:pw123@hub.example",
	} {
		tctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond) // a regression dials: bound it
		err := ConnectHub(tctx, u, "t")
		cancel()
		require.Error(t, err, u)
		assert.ErrorContains(t, err, "varmq: hub url", u)
		assert.NotContains(t, err.Error(), "pw123", u)
	}

	// plain http to a loopback host, or anywhere with the option, dials
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	assert.ErrorIs(t, ConnectHub(cctx, "http://localhost:1", "t"), context.Canceled)
	assert.ErrorIs(t, ConnectHub(cctx, "http://example.com", "t", WithHubInsecure()), context.Canceled)
}

func TestHubTiming(t *testing.T) {
	h := &hub{cfg: newHubConfig(nil)}
	assert.Equal(t, 5*time.Second, h.clampBeat(0), "hello gives none: default")
	assert.Equal(t, time.Second, h.clampBeat(10))
	assert.Equal(t, 60*time.Second, h.clampBeat(600000))
	assert.Equal(t, 2500*time.Millisecond, h.clampBeat(2500))
	assert.Equal(t, time.Second, h.clampBeat(-5), "only absent or 0 is the default")
	assert.Equal(t, 2*time.Second, (&hub{cfg: newHubConfig([]HubOption{WithHubHeartbeat(2 * time.Second)})}).clampBeat(0))

	d := h.nextBackoff(0, false)
	assert.Equal(t, time.Second, d, "first retry within 0.5-1 s")
	var seq []time.Duration
	for range 7 {
		d = h.nextBackoff(d, false)
		seq = append(seq, d)
	}
	assert.Equal(t, []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}, seq)
	assert.Equal(t, 30*time.Second, h.nextBackoff(0, true), "401 and 400 retry at the cap")
}

// hubBlockingStore's Jobs blocks until release, counting calls at once.
type hubBlockingStore struct {
	hubJobStore
	release      chan struct{}
	entered      chan struct{}
	now, highest atomic.Int32
}

func (s *hubBlockingStore) Jobs(ctx context.Context, st JobState, o, l int) ([]JobRecord, error) {
	n := s.now.Add(1)
	defer s.now.Add(-1)
	for {
		h := s.highest.Load()
		if n <= h || s.highest.CompareAndSwap(h, n) {
			break
		}
	}
	s.entered <- struct{}{}
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return nil, nil
}

func TestHubConcurrencyCap(t *testing.T) {
	st := &hubBlockingStore{hubJobStore: hubJobStore{"custom:hub-block"}, release: make(chan struct{}), entered: make(chan struct{}, 32)}
	queueRegistry.Store(st.id, st)
	t.Cleanup(func() { queueRegistry.Delete(st.id) })
	f := newFakeHub(t)
	f.connect(t)
	s := within(t, f.streams)

	const n = 12
	for i := range n {
		s.request("c"+strconv.Itoa(i), "GET", "/queues/"+st.id+"/jobs?state=pending")
	}
	for range hubMaxInFlight {
		within(t, st.entered)
	}
	select {
	case <-st.entered:
		t.Fatal("a 9th request ran")
	case <-time.After(50 * time.Millisecond):
	}
	close(st.release)
	seen := map[string]bool{}
	for range n {
		r := within(t, f.replies)
		assert.Equal(t, "200", r.status)
		seen[r.id] = true
	}
	assert.Len(t, seen, n)
	assert.EqualValues(t, hubMaxInFlight, st.highest.Load())
}

// hubHugeStore lists one job whose payload passes the 16 MiB reply cap.
type hubHugeStore struct{ hubJobStore }

func (hubHugeStore) Jobs(context.Context, JobState, int, int) ([]JobRecord, error) {
	return []JobRecord{{ID: "big", State: JobPending, Payload: json.RawMessage(`"` + strings.Repeat("a", hubMaxReply) + `"`)}}, nil
}

func TestHubOversizeReply(t *testing.T) {
	st := hubHugeStore{hubJobStore{"custom:hub-huge"}}
	queueRegistry.Store(st.id, st)
	t.Cleanup(func() { queueRegistry.Delete(st.id) })
	f := newFakeHub(t)
	f.connect(t)
	s := within(t, f.streams)

	s.request("big", "GET", "/queues/"+st.id+"/jobs?state=pending")
	r := f.reply(t, "big")
	assert.Equal(t, "502", r.status)
	assert.Equal(t, "application/json", r.ctype)
	assert.JSONEq(t, `{"error":"reply over 16 MiB"}`, r.body)
	assert.Equal(t, "hub-test", r.varmqID)
}

// ServeMux unescapes the path, so every spelling of a job route reaches
// the job handlers: no-payload mode holds for all of them.
func TestHubNoPayloadsEscapedPaths(t *testing.T) {
	queueRegistry.Store("hub-esc", hubJobStore{"hub-esc"})
	t.Cleanup(func() { queueRegistry.Delete("hub-esc") })
	f := newFakeHub(t)
	f.connect(t, WithHubNoPayloads())
	s := within(t, f.streams)

	for i, p := range []string{
		"/queues/hub-esc/%6Aobs?state=completed",
		"/%71ueues/hub-esc/jobs?state=completed",
		"/queues/hub-esc/%6Aobs/j",
		"/queues/hub-esc/job%73/j",
		"/%71ueues/hub-esc/jobs/j",
	} {
		id := "e" + strconv.Itoa(i)
		s.request(id, "GET", p)
		r := f.reply(t, id)
		require.Equal(t, "200", r.status, p)
		assert.NotContains(t, r.body, "secret", p)
		assert.Contains(t, r.body, `"payload":null`, p)
	}
}

// hubBigPageStore lists 300 jobs of 60 KiB payload each: 18 MB raw, small
// without payloads.
type hubBigPageStore struct{ hubJobStore }

func (hubBigPageStore) Jobs(context.Context, JobState, int, int) ([]JobRecord, error) {
	out := make([]JobRecord, 300)
	p := json.RawMessage(`"` + strings.Repeat("a", 60<<10) + `"`)
	for i := range out {
		out[i] = JobRecord{ID: "x" + strconv.Itoa(i), State: JobPending, Payload: p}
	}
	return out, nil
}

// The 16 MiB cap applies to what is sent: a page over it raw but small
// without payloads is answered.
func TestHubNoPayloadsBigPage(t *testing.T) {
	st := hubBigPageStore{hubJobStore{"custom:hub-bigpage"}}
	queueRegistry.Store(st.id, st)
	t.Cleanup(func() { queueRegistry.Delete(st.id) })
	f := newFakeHub(t)
	f.connect(t, WithHubNoPayloads())
	s := within(t, f.streams)

	s.request("p", "GET", "/queues/"+st.id+"/jobs?state=pending&limit=300")
	r := f.reply(t, "p")
	require.Equal(t, "200", r.status, "%.80s", r.body)
	assert.Equal(t, 300, strings.Count(r.body, `"payload":null`))
	assert.NotContains(t, r.body, "aaaa")
}

// No client follows a redirect: the token goes to the hub URL only.
func TestHubRedirectNotFollowed(t *testing.T) {
	old := InstanceID
	InstanceID = "hub-test"
	t.Cleanup(func() { InstanceID = old })
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	redirect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	})
	tlsSrc := httptest.NewTLSServer(redirect)
	t.Cleanup(tlsSrc.Close)
	plainSrc := httptest.NewServer(redirect)
	t.Cleanup(plainSrc.Close)

	caller := &http.Client{Transport: tlsSrc.Client().Transport}
	for _, c := range []struct {
		name, url string
		opts      []HubOption
	}{
		{"caller client, https to http", tlsSrc.URL, []HubOption{WithHubClient(caller)}},
		{"own client", plainSrc.URL, nil},
	} {
		errs := make(chan error, 64)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		opts := append(c.opts, withHubTiming(10*time.Millisecond, 10*time.Millisecond, 40*time.Millisecond),
			WithHubErrHandler(func(err error) {
				select {
				case errs <- err:
				default:
				}
			}))
		go func() { done <- ConnectHub(ctx, c.url, "s3cret", opts...) }()
		errIs(t, errs, hubStatus("stream", http.StatusTemporaryRedirect))
		cancel()
		within(t, done)
		assert.Zero(t, hits.Load(), c.name)
	}
	assert.Nil(t, caller.CheckRedirect, "the caller's client is not changed")
}

// A handler that ignores ctx (Restart waits for the running job) blocks
// neither the redial nor the return of ConnectHub.
func TestHubStuckHandler(t *testing.T) {
	WorkerRegistry.Clear()
	f := newFakeHub(t)
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	w := NewWorker(func(Job[int]) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	}, WithName("hub-slow"))
	t.Cleanup(func() { w.Stop(); WorkerRegistry.Clear() })
	w.BindQueue().Add(1)
	within(t, started)

	_, stop := f.connect(t)
	s := within(t, f.streams)
	s.request("r", "PATCH", "/workers/hub-slow/actions/restart")
	time.Sleep(50 * time.Millisecond) // the restart waits for the job
	close(s.quit)

	s = within(t, f.streams) // redialed
	s.request("h", "GET", "/health")
	assert.Equal(t, "200", f.reply(t, "h").status, "the other runners serve")

	close(release)
	f.runnersFree(t, s) // the restart returned; its reply was dropped
	assert.Empty(t, f.replies)
	assert.ErrorIs(t, stop(), context.Canceled)
}

// ConnectHub returns on cancel while a handler ignores ctx. The handler
// ends after the return: no reply, and its panic reaches no callback.
func TestHubStuckHandlerStop(t *testing.T) {
	st := &hubStuckStore{hubJobStore{"custom:hub-stuck-stop"}, make(chan struct{}), make(chan struct{}, 32), true}
	queueRegistry.Store(st.id, st)
	t.Cleanup(func() { queueRegistry.Delete(st.id) })
	f := newFakeHub(t)
	var errs chan error
	t.Cleanup(func() { // after connect's cleanup: the runners are done
		assert.Empty(t, errs, "callback after ConnectHub returned")
		assert.Empty(t, f.replies)
	})
	errs, stop := f.connect(t)
	s := within(t, f.streams)

	s.request("x", "GET", "/queues/"+st.id+"/jobs?state=pending")
	within(t, st.entered)
	assert.ErrorIs(t, stop(), context.Canceled, "returns while the handler waits")
	close(st.release)
}

// hubBarrierStore's Jobs returns once all 8 runners are in it.
type hubBarrierStore struct {
	hubJobStore
	n   atomic.Int32
	all chan struct{}
}

func (s *hubBarrierStore) Jobs(ctx context.Context, _ JobState, _, _ int) ([]JobRecord, error) {
	if s.n.Add(1) == hubMaxInFlight {
		close(s.all)
	}
	select {
	case <-s.all:
	case <-ctx.Done():
	}
	return nil, nil
}

// runnersFree waits until all 8 runners serve on s at once: every handler
// stuck before has returned (and read InstanceID, restored at cleanup).
func (f *fakeHub) runnersFree(t *testing.T, s *hubStream) {
	t.Helper()
	st := &hubBarrierStore{hubJobStore: hubJobStore{"custom:hub-barrier"}, all: make(chan struct{})}
	queueRegistry.Store(st.id, st)
	defer queueRegistry.Delete(st.id)
	for i := range hubMaxInFlight {
		s.request("free"+strconv.Itoa(i), "GET", "/queues/"+st.id+"/jobs?state=pending")
	}
	for range hubMaxInFlight {
		assert.Equal(t, "200", within(t, f.replies).status)
	}
}

// hubStuckStore's Jobs ignores ctx: it returns only on release, or panics
// then when panics is set.
type hubStuckStore struct {
	hubJobStore
	release chan struct{}
	entered chan struct{}
	panics  bool
}

func (s *hubStuckStore) Jobs(context.Context, JobState, int, int) ([]JobRecord, error) {
	s.entered <- struct{}{}
	<-s.release
	if s.panics {
		panic("late")
	}
	return nil, nil
}

// The 8-request cap holds across redials: handlers stuck past their stream
// still count, and their replies are dropped.
func TestHubCapAcrossRedials(t *testing.T) {
	st := &hubStuckStore{hubJobStore{"custom:hub-stuck"}, make(chan struct{}), make(chan struct{}, 32), false}
	queueRegistry.Store(st.id, st)
	t.Cleanup(func() { queueRegistry.Delete(st.id) })
	f := newFakeHub(t)
	f.connect(t)
	release := sync.OnceFunc(func() { close(st.release) })
	t.Cleanup(release)
	path := "/queues/" + st.id + "/jobs?state=pending"

	s := within(t, f.streams)
	for i := range hubMaxInFlight {
		s.request("a"+strconv.Itoa(i), "GET", path)
	}
	for range hubMaxInFlight {
		within(t, st.entered)
	}
	close(s.quit)

	s = within(t, f.streams)
	s.request("b", "GET", path)
	select {
	case <-st.entered:
		t.Fatal("a 9th request ran across the redial")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	within(t, st.entered)
	r := f.reply(t, "b") // the first reply: the stuck ones were dropped
	assert.Equal(t, "200", r.status)
	f.runnersFree(t, s)
	assert.Empty(t, f.replies)
}

// hubFreezeProxy forwards TCP to target. freeze black-holes every
// connection open at that moment: no data, no FIN, like a lost NAT entry.
type hubFreezeProxy struct {
	ln     net.Listener
	target string
	done   chan struct{}
	mu     sync.Mutex
	conns  []net.Conn
	gates  []*atomic.Bool
}

func (p *hubFreezeProxy) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		up, err := net.Dial("tcp", p.target)
		if err != nil {
			c.Close()
			continue
		}
		g := new(atomic.Bool)
		p.mu.Lock()
		p.conns = append(p.conns, c, up)
		p.gates = append(p.gates, g)
		p.mu.Unlock()
		pipe := func(dst, src net.Conn) {
			buf := make([]byte, 32<<10)
			for {
				n, err := src.Read(buf)
				if g.Load() {
					<-p.done
					return
				}
				if n > 0 {
					dst.Write(buf[:n])
				}
				if err != nil {
					dst.Close()
					return
				}
			}
		}
		go pipe(up, c)
		go pipe(c, up)
	}
}

func (p *hubFreezeProxy) freeze() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, g := range p.gates {
		g.Store(true)
	}
}

func (p *hubFreezeProxy) close() {
	close(p.done)
	p.ln.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		c.Close()
	}
}

// A dead HTTP/2 connection (no FIN) is not reused for the redial.
func TestHubHTTP2HalfOpenRedials(t *testing.T) {
	old := InstanceID
	InstanceID = "hub-test"
	t.Cleanup(func() { InstanceID = old })
	var streams atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hub/stream", func(w http.ResponseWriter, r *http.Request) {
		streams.Add(1)
		w.Header().Set("Content-Type", "application/x-ndjson")
		fl := w.(http.Flusher)
		io.WriteString(w, strings.ReplaceAll(hubHello(20), "$SESSION", hubSession())+"\n")
		fl.Flush()
		tk := time.NewTicker(10 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-tk.C:
				io.WriteString(w, `{"type":"ping"}`+"\n")
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("POST /hub/beat", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &hubFreezeProxy{ln: ln, target: srv.Listener.Addr().String(), done: make(chan struct{})}
	go p.serve()

	tr := &http.Transport{
		TLSClientConfig:   srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone(),
		ForceAttemptHTTP2: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ConnectHub(ctx, "https://"+ln.Addr().String(), "tok", WithHubClient(&http.Client{Transport: tr}),
			withHubTiming(10*time.Millisecond, 10*time.Millisecond, 40*time.Millisecond))
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second): // bye may wait its 2 s
			t.Error("ConnectHub did not return")
		}
		p.close()
		tr.CloseIdleConnections()
		srv.Close()
	})

	waitFor := func(cond func() bool, msg string) {
		t.Helper()
		// 5 s: the bye before the redial may ride the dead connection for its 2 s
		for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal(msg)
			}
		}
	}
	waitFor(func() bool { return streams.Load() > 0 }, "no stream")
	time.Sleep(30 * time.Millisecond) // pings flow
	s0 := streams.Load()
	p.freeze()
	waitFor(func() bool { return streams.Load() > s0 }, "no new stream after the connection went dead")
}

// A reply POST that the hub never answers ends after the reply timeout,
// and its runner serves again.
func TestHubReplyTimeout(t *testing.T) {
	f := newFakeHub(t)
	f.replyHang.Store(true)
	errs, _ := f.connect(t, func(c *hubConfig) { c.replyTimeout = 50 * time.Millisecond })
	s := within(t, f.streams)

	s.request("slow", "GET", "/health")
	err := errIs(t, errs, func(err error) bool { return strings.Contains(err.Error(), "hub reply") })
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	f.replyHang.Store(false)
	s.request("ok", "GET", "/health")
	assert.Equal(t, "200", f.reply(t, "ok").status)
}

// Bad frames are reported with fixed or truncated text; 16 of them end the
// stream, so the redial backoff bounds the callback rate.
func TestHubBadFrames(t *testing.T) {
	f := newFakeHub(t)
	errs, _ := f.connect(t)
	s := within(t, f.streams)

	s.request(strings.Repeat("A", 60000)+"!", "GET", "/health")
	err := within(t, errs)
	assert.Contains(t, err.Error(), "bad request id")
	assert.Less(t, len(err.Error()), 200, "hub text truncated")
	s.send(`{"type":"request","id":5}`)
	err = within(t, errs)
	assert.Less(t, len(err.Error()), 200)
	for range hubMaxBadFrames - 2 {
		s.send("garbage " + strings.Repeat("B", 1000))
	}
	errIs(t, errs, func(err error) bool { return errors.Is(err, errHubBadFrames) })
	within(t, f.streams) // redialed
}

// hubPanicStore's Jobs panics with a long value.
type hubPanicStore struct{ hubJobStore }

func (hubPanicStore) Jobs(context.Context, JobState, int, int) ([]JobRecord, error) {
	panic("boom " + strings.Repeat("x", 1000))
}

// A handler panic is answered 500 and reported, truncated; the runner
// serves again.
func TestHubHandlerPanic(t *testing.T) {
	st := hubPanicStore{hubJobStore{"custom:hub-panic"}}
	queueRegistry.Store(st.id, st)
	t.Cleanup(func() { queueRegistry.Delete(st.id) })
	f := newFakeHub(t)
	errs, _ := f.connect(t)
	s := within(t, f.streams)

	s.request("p", "GET", "/queues/"+st.id+"/jobs?state=pending")
	r := f.reply(t, "p")
	assert.Equal(t, "500", r.status)
	assert.JSONEq(t, `{"error":"handler panicked"}`, r.body)
	err := errIs(t, errs, func(err error) bool { return strings.Contains(err.Error(), "panicked") })
	assert.Contains(t, err.Error(), "boom")
	assert.Less(t, len(err.Error()), 300, "panic value truncated")

	s.request("h", "GET", "/health")
	assert.Equal(t, "200", f.reply(t, "h").status)
}

// A line of an unknown type is ignored whole, even with wrongly typed
// fields that a known type has.
func TestHubUnknownTypeSilent(t *testing.T) {
	f := newFakeHub(t)
	errs, _ := f.connect(t)
	s := within(t, f.streams)

	s.send(`{"type":"config","id":5,"method":[1],"protocol":"x","heartbeat_ms":{}}`, `{"no":"type"}`)
	s.request("h", "GET", "/health")
	assert.Equal(t, "200", f.reply(t, "h").status)
	select {
	case err := <-errs:
		t.Fatalf("reported: %v", err)
	default:
	}
}

// A reader stuck on a full request queue ends the stream with an error
// that says so, not "no line".
func TestHubQueueFull(t *testing.T) {
	st := &hubBlockingStore{hubJobStore: hubJobStore{"custom:hub-full"}, release: make(chan struct{}), entered: make(chan struct{}, 256)}
	queueRegistry.Store(st.id, st)
	t.Cleanup(func() { queueRegistry.Delete(st.id) })
	f := newFakeHub(t)
	f.hello.Store(hubHello(50))
	errs, _ := f.connect(t)
	s := within(t, f.streams)

	for i := range hubMaxInFlight + hubQueued + 1 {
		s.request("f"+strconv.Itoa(i), "GET", "/queues/"+st.id+"/jobs?state=pending")
	}
	err := errIs(t, errs, func(err error) bool { return errors.Is(err, errHubBusy) || errors.Is(err, errHubStale) })
	assert.ErrorIs(t, err, errHubBusy)
	assert.Contains(t, err.Error(), "queue")
}

// heartbeat_ms in hello sets the beat; absent, the configured one does.
func TestHubHelloHeartbeat(t *testing.T) {
	t.Run("from hello", func(t *testing.T) {
		f := newFakeHub(t)
		f.hello.Store(hubHello(30))
		f.connect(t) // configured: 5 s
		within(t, f.streams)
		within(t, f.beats) // the first, at once
		assert.Empty(t, within(t, f.beats).Get("bye"))
	})
	t.Run("absent", func(t *testing.T) {
		f := newFakeHub(t)
		f.hello.Store(`{"type":"hello","protocol":1,"session":"$SESSION"}`)
		f.connect(t, WithHubHeartbeat(30*time.Millisecond))
		within(t, f.streams)
		within(t, f.beats)
		assert.Empty(t, within(t, f.beats).Get("bye"))
	})
}

// A second hello is ignored: its heartbeat is not applied.
func TestHubSecondHello(t *testing.T) {
	f := newFakeHub(t) // hello: 5 s
	errs, _ := f.connect(t)
	s := within(t, f.streams)

	s.hello(10) // applied, no pings: stale in 30 ms
	time.Sleep(100 * time.Millisecond)
	s.request("h", "GET", "/health")
	assert.Equal(t, "200", f.reply(t, "h").status)
	within(t, f.beats) // the first beat, at once
	assert.Empty(t, f.beats, "next beat at 5 s")
	assert.Empty(t, f.streams, "no redial")
	assert.Empty(t, errs)
}

// A hello of another protocol ends the stream; with no hello taken, stop
// sends no bye.
func TestHubProtocolMismatch(t *testing.T) {
	f := newFakeHub(t)
	f.hello.Store(`{"type":"hello","protocol":2,"heartbeat_ms":5000}`)
	errs, stop := f.connect(t)
	err := errIs(t, errs, func(err error) bool { return strings.Contains(err.Error(), "protocol 2") })
	assert.Contains(t, err.Error(), "want 1")
	within(t, f.streams)
	within(t, f.streams) // redials
	assert.ErrorIs(t, stop(), context.Canceled)
	for len(f.beats) > 0 {
		assert.NotEqual(t, "1", (<-f.beats).Get("bye"))
	}
}

// A stream with no hello runs no request and sends no beat, and stop
// sends no bye.
func TestHubNoHelloNoBye(t *testing.T) {
	f := newFakeHub(t)
	f.hello.Store("")
	errs, stop := f.connect(t)
	s := within(t, f.streams)
	s.request("h", "GET", "/health")
	errIs(t, errs, func(err error) bool { return errors.Is(err, errHubEarlyReq) })
	assert.ErrorIs(t, stop(), context.Canceled)
	assert.Empty(t, f.replies, "dropped")
	assert.Empty(t, f.beats, "no beat, no bye")
}

// A request before hello is dropped; one after it runs and its reply
// carries the session.
func TestHubRequestBeforeHello(t *testing.T) {
	f := newFakeHub(t)
	f.hello.Store("")
	f.connect(t)
	s := within(t, f.streams)
	s.request("early", "GET", "/health")
	s.hello(5000)
	s.request("late", "GET", "/health")
	r := f.reply(t, "late") // the first reply: early was dropped
	assert.Equal(t, "200", r.status)
	assert.Equal(t, s.session, r.session)
}

// Beats and replies carry the stream's session in X-Hub-Session, never in
// a URL; the first beat comes at once after hello, not one heartbeat on.
func TestHubSession(t *testing.T) {
	f := newFakeHub(t)
	f.hello.Store(hubHello(60000))
	errs, stop := f.connect(t)
	s := within(t, f.streams)
	assert.NotContains(t, s.query.Encode(), s.session)

	select {
	case b := <-f.beats:
		assert.Equal(t, s.session, b.session)
		assert.Empty(t, b.Get("bye"))
		assert.NotContains(t, b.Encode(), s.session)
	case <-time.After(2 * time.Second): // a heartbeat is 60 s
		t.Fatal("no first beat right after hello")
	}

	s.request("r", "GET", "/health")
	r := f.reply(t, "r")
	assert.Equal(t, s.session, r.session)
	assert.NotContains(t, r.url, s.session)

	assert.ErrorIs(t, stop(), context.Canceled)
	b := within(t, f.beats)
	assert.Equal(t, "1", b.Get("bye"))
	assert.Equal(t, s.session, b.session)
	for len(errs) > 0 {
		assert.NotContains(t, (<-errs).Error(), s.session)
	}
}

// A hello without a valid session (22 to 64 bytes of [A-Za-z0-9_-]) ends
// the stream: the instance redials, sends no beat and no bye, and its
// error never repeats the value.
func TestHubHelloBadSession(t *testing.T) {
	for name, c := range map[string]struct{ hello, leak string }{
		"missing":   {`{"type":"hello","protocol":1,"heartbeat_ms":5000}`, ""},
		"empty":     {`{"type":"hello","protocol":1,"heartbeat_ms":5000,"session":""}`, ""},
		"21 bytes":  {`{"type":"hello","protocol":1,"heartbeat_ms":5000,"session":"LEAK` + strings.Repeat("a", 17) + `"}`, "LEAK"},
		"65 bytes":  {`{"type":"hello","protocol":1,"heartbeat_ms":5000,"session":"LEAK` + strings.Repeat("a", 61) + `"}`, "LEAK"},
		"bad char":  {`{"type":"hello","protocol":1,"heartbeat_ms":5000,"session":"LEAK` + strings.Repeat("a", 20) + `.x"}`, "LEAK"},
		"not text":  {`{"type":"hello","protocol":1,"heartbeat_ms":5000,"session":5}`, ""},
		"null byte": {`{"type":"hello","protocol":1,"heartbeat_ms":5000,"session":"LEAK` + strings.Repeat("a", 20) + `\u0000"}`, "LEAK"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeHub(t)
			f.hello.Store(c.hello)
			errs, stop := f.connect(t)
			s := within(t, f.streams)
			s.request("r", "GET", "/health") // may arrive before the stream ends
			err := errIs(t, errs, func(err error) bool {
				return errors.Is(err, errHubBadHello) || strings.Contains(err.Error(), "bad hello fields")
			})
			if c.leak != "" {
				assert.NotContains(t, err.Error(), c.leak)
			}
			within(t, f.streams) // redialed
			assert.ErrorIs(t, stop(), context.Canceled)
			assert.Empty(t, f.replies, "no session, no reply")
			assert.Empty(t, f.beats, "no session, no beat, no bye")
		})
	}
	for _, n := range []int{22, 64} { // the bounds are valid
		f := newFakeHub(t)
		sess := strings.Repeat("A_-9", 16)[:n]
		f.hello.Store(`{"type":"hello","protocol":1,"heartbeat_ms":5000,"session":"` + sess + `"}`)
		f.connect(t)
		within(t, f.streams)
		assert.Equal(t, sess, within(t, f.beats).session, n)
	}
}

// When a stream that received hello ends, the instance says bye with that
// stream's session before it redials; the new stream has a new session.
func TestHubByeBeforeRedial(t *testing.T) {
	f := newFakeHub(t)
	errs, stop := f.connect(t)
	s1 := within(t, f.streams)
	assert.Equal(t, s1.session, within(t, f.beats).session) // first beat
	close(s1.quit)
	errIs(t, errs, func(err error) bool { return errors.Is(err, errHubClosed) })
	s2 := within(t, f.streams)
	assert.NotEqual(t, s1.session, s2.session)

	var bye *hubBeat // posted before the redial: already in f.beats
	for len(f.beats) > 0 && bye == nil {
		if b := <-f.beats; b.Get("bye") == "1" {
			bye = &b
		}
	}
	require.NotNil(t, bye, "no bye before the redial")
	assert.Equal(t, s1.session, bye.session, "bye carries the old session")

	s2.request("r", "GET", "/health")
	assert.Equal(t, s2.session, f.reply(t, "r").session)
	assert.ErrorIs(t, stop(), context.Canceled)
	for {
		if b := within(t, f.beats); b.Get("bye") == "1" {
			assert.Equal(t, s2.session, b.session)
			return
		}
	}
}

// A 64 KiB line is read; a longer one ends the stream.
func TestHubLineCap(t *testing.T) {
	f := newFakeHub(t)
	errs, _ := f.connect(t)
	s := within(t, f.streams)
	line := func(n int) string {
		const head = `{"type":"ping","pad":""}`
		return `{"type":"ping","pad":"` + strings.Repeat("x", n-len(head)) + `"}`
	}

	s.send(line(hubMaxLine))
	s.request("ok", "GET", "/health")
	assert.Equal(t, "200", f.reply(t, "ok").status)
	s.send(line(hubMaxLine + 1))
	errIs(t, errs, func(err error) bool { return errors.Is(err, bufio.ErrTooLong) })
	within(t, f.streams)
}

// A beat answered 401 ends the stream, then the instance retries.
func TestHubBeatUnauthorized(t *testing.T) {
	f := newFakeHub(t)
	f.hello.Store(hubHello(20))
	f.beatStatus.Store(http.StatusUnauthorized)
	errs, _ := f.connect(t)
	within(t, f.streams)
	errIs(t, errs, hubStatus("beat", http.StatusUnauthorized))
	within(t, f.streams)
}

// A reply the hub answers with an unexpected status is reported; the
// stream stays.
func TestHubReplyStatus(t *testing.T) {
	f := newFakeHub(t)
	f.replyStatus.Store(http.StatusRequestEntityTooLarge)
	errs, _ := f.connect(t)
	s := within(t, f.streams)
	s.request("r", "GET", "/health")
	f.reply(t, "r")
	errIs(t, errs, hubStatus("reply", http.StatusRequestEntityTooLarge))

	f.replyStatus.Store(0)
	s.request("ok", "GET", "/health")
	assert.Equal(t, "200", f.reply(t, "ok").status)
}

// A hub that fails every reply gets one report at once, then at most one
// per heartbeat with the count of the rest.
func TestHubReplyErrorRate(t *testing.T) {
	f := newFakeHub(t)
	f.hello.Store(hubHello(5000))
	f.replyStatus.Store(http.StatusInternalServerError)
	errs, _ := f.connect(t)
	s := within(t, f.streams)
	for i := range 20 {
		id := "r" + strconv.Itoa(i)
		s.request(id, "GET", "/health")
		f.reply(t, id)
	}
	errIs(t, errs, hubStatus("reply", http.StatusInternalServerError))
	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, errs, "reports inside one heartbeat are suppressed")
}

// A stream 409 (ID taken) retries with the normal backoff, not the cap.
func TestHubConflictBackoff(t *testing.T) {
	f := newFakeHub(t)
	f.streamStatus.Store(http.StatusConflict)
	errs, _ := f.connect(t, withHubTiming(10*time.Millisecond, 10*time.Millisecond, time.Hour))
	for range 3 {
		errIs(t, errs, hubStatus("stream", http.StatusConflict))
	}
}

// A stream 503 (hub at its instance limit, rate limit or memory budget)
// retries with the normal backoff, not the cap.
func TestHubUnavailableBackoff(t *testing.T) {
	f := newFakeHub(t)
	f.streamStatus.Store(http.StatusServiceUnavailable)
	errs, _ := f.connect(t, withHubTiming(10*time.Millisecond, 10*time.Millisecond, time.Hour))
	for range 3 {
		errIs(t, errs, hubStatus("stream", http.StatusServiceUnavailable))
	}
	f.streamStatus.Store(0)
	within(t, f.streams)
}

// hubCountingTransport counts the requests it carries.
type hubCountingTransport struct {
	n  atomic.Int32
	rt http.RoundTripper
}

func (c *hubCountingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return c.rt.RoundTrip(r)
}

// WithHubClient: its transport carries every call; its Timeout is not
// applied, and the caller's client is not changed.
func TestHubWithClient(t *testing.T) {
	f := newFakeHub(t)
	tr := &hubCountingTransport{rt: f.srv.Client().Transport}
	c := &http.Client{Transport: tr, Timeout: time.Millisecond}
	f.connect(t, WithHubClient(c))
	s := within(t, f.streams)

	time.Sleep(20 * time.Millisecond) // past c.Timeout: the stream lives
	s.request("h", "GET", "/health")
	assert.Equal(t, "200", f.reply(t, "h").status)
	assert.GreaterOrEqual(t, tr.n.Load(), int32(2), "stream and reply")
	assert.Empty(t, f.streams, "no redial")
	assert.Equal(t, time.Millisecond, c.Timeout)
	assert.Nil(t, c.CheckRedirect)
}
