package varmq

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
	time.Sleep(100 * time.Millisecond) // Give time to spin up idle workers

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
				if !w.IsStopped() {
					t.Errorf("expected worker to be stopped")
				}
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
