package varmq

import (
	"context"
	"runtime/debug"
	"testing"
	"time"

	"github.com/goptics/varmq/contract"
	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// init registers instanceRecord, which lists named workers and their Queue IDs.
func TestInstanceRecord(t *testing.T) {
	WorkerRegistry.Clear()
	defer WorkerRegistry.Clear()
	oldID := InstanceID
	InstanceID = "pod-a"
	defer func() { InstanceID = oldID }()

	w := NewWorker(func(Job[string]) {}, WithName("emails"), WithConcurrency(3))
	defer w.Stop()
	w.WithQueue(&inspectedQueue{MockPersistentQueue: mocks.NewMockPersistentQueue(), id: "redisq:prio:{emails}"})
	w.WithQueue(mocks.NewMockPersistentQueue()) // no Queue ID, left out
	w.BindQueue()                               // in-memory: not readable by other processes, left out
	w.BindPriorityQueue()

	r, ok := contract.InstanceSnapshot() // init registered instanceRecord
	require.True(t, ok)
	assert.Equal(t, "pod-a", r.ID)
	assert.Equal(t, processStart.UnixMilli(), r.StartedAt)
	assert.Zero(t, r.IntervalMS) // the reporter's to fill
	require.Len(t, r.Workers, 1)
	assert.Equal(t, "emails", r.Workers[0].Name)
	assert.Equal(t, w.Status(), r.Workers[0].Status)
	assert.Equal(t, 3, r.Workers[0].Concurrency)
	assert.Equal(t, []string{"redisq:prio:{emails}"}, r.Workers[0].QueueIDs)
}

// plainWorker is a Worker that is not varmq's *worker, so it has no bound
// Queue IDs to report.
type plainWorker struct{ Worker }

// Registry entries that are not varmq's *worker: a Worker without Queue IDs,
// and a value that is not a Worker at all.
func TestInstanceRecordForeignEntries(t *testing.T) {
	WorkerRegistry.Clear()
	defer WorkerRegistry.Clear()

	w := NewWorker(func(Job[string]) {}, WithName("real"))
	defer w.Stop()
	WorkerRegistry.Delete("real")
	WorkerRegistry.Store("plain", plainWorker{w})
	WorkerRegistry.Store("junk", "not a worker") // skipped
	first := NewWorker(func(Job[string]) {}, WithName("a-first"))
	defer first.Stop()

	r := instanceRecord()
	require.Len(t, r.Workers, 2)
	assert.Equal(t, "a-first", r.Workers[0].Name) // sorted by name
	assert.Equal(t, "real", r.Workers[1].Name)    // Name comes from the wrapped worker
	assert.Equal(t, []string{}, r.Workers[1].QueueIDs)
}

// A QueueInspector with an empty Queue ID is not listed in queue_ids.
func TestBindSkipsEmptyQueueID(t *testing.T) {
	w := newWorker(func(iJob[string]) {})
	w.bind(&inspectedQueue{MockPersistentQueue: mocks.NewMockPersistentQueue(), id: ""}, 0)
	assert.Equal(t, []string{}, w.boundQueueIDs())
}

// depVersion finds varmq among the build's dependencies.
func TestDepVersion(t *testing.T) {
	assert.Empty(t, depVersion(nil, false))
	bi := &debug.BuildInfo{Deps: []*debug.Module{{Path: "example.com/x", Version: "v9"}}}
	assert.Empty(t, depVersion(bi, true))
	bi.Deps = append(bi.Deps, &debug.Module{Path: "github.com/goptics/varmq", Version: "v1.11.0"})
	assert.Equal(t, "v1.11.0", depVersion(bi, true))
}

// recordingRegistry sends each reported record on reports.
type recordingRegistry struct{ reports chan InstanceRecord }

func (r recordingRegistry) ReportInstance(_ context.Context, rec InstanceRecord) error {
	r.reports <- rec
	return nil
}
func (recordingRegistry) RemoveInstance(context.Context, string) error { return nil }
func (recordingRegistry) Instances(context.Context) ([]InstanceRecord, error) {
	return nil, nil
}

// Lifecycle changes (pause, resume, tune, stop) are reported at once, long
// before the 60s tick, with the new status and concurrency; the record also
// carries the worker's processed counts.
func TestLifecycleReportsAtOnce(t *testing.T) {
	WorkerRegistry.Clear()
	defer WorkerRegistry.Clear()
	oldID := InstanceID
	InstanceID = "pod-life"
	defer func() { InstanceID = oldID }()

	w := NewWorker(func(Job[int]) {}, WithName("life"), WithConcurrency(2))
	q := w.BindQueue()
	q.Add(1)
	w.Wait()

	reg := recordingRegistry{reports: make(chan InstanceRecord, 50)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = contract.ReportInstances(ctx, reg, time.Minute) }()
	defer func() { cancel(); <-done }() // before InstanceID is restored: the reporter reads it

	next := func(t *testing.T) InstanceWorker {
		t.Helper()
		select {
		case r := <-reg.reports:
			require.Len(t, r.Workers, 1)
			return r.Workers[0]
		case <-time.After(2 * time.Second):
			t.Fatal("no report within 2s")
			return InstanceWorker{}
		}
	}

	first := next(t)
	assert.Equal(t, uint64(1), first.Completed)
	assert.Equal(t, uint64(1), first.Successful)
	assert.Zero(t, first.Failed)

	require.NoError(t, w.Pause())
	assert.Equal(t, "Paused", next(t).Status)
	require.NoError(t, w.Resume())
	assert.Equal(t, "Idle", next(t).Status)
	require.NoError(t, w.TunePool(5))
	assert.Equal(t, 5, next(t).Concurrency)

	require.NoError(t, w.Stop())
	for s := next(t).Status; s != "Stopped"; s = next(t).Status { // Stopping, then Stopped
		assert.Equal(t, "Stopping", s)
	}
}
