package varmq

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
