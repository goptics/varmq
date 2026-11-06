package varmq

import (
	"runtime/debug"
	"testing"

	"github.com/goptics/varmq/contract"
	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstanceRecord(t *testing.T) {
	WorkerRegistry.Clear()
	defer WorkerRegistry.Clear()
	oldID := InstanceID
	InstanceID = "pod-a"
	defer func() { InstanceID = oldID }()

	w := NewWorker(func(Job[string]) {}, WithName("emails"), WithConcurrency(3))
	defer w.Stop()
	w.WithPersistentQueue(&inspectedQueue{MockPersistentQueue: mocks.NewMockPersistentQueue(), id: "redisq:prio:{emails}"})
	w.WithPersistentQueue(mocks.NewMockPersistentQueue()) // no Queue ID, left out

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

func TestInstanceRecordForeignEntries(t *testing.T) {
	WorkerRegistry.Clear()
	defer WorkerRegistry.Clear()

	w := NewWorker(func(Job[string]) {}, WithName("real"))
	defer w.Stop()
	WorkerRegistry.Delete("real")
	WorkerRegistry.Store("plain", plainWorker{w})
	WorkerRegistry.Store("junk", "not a worker") // skipped

	r := instanceRecord()
	require.Len(t, r.Workers, 1)
	assert.Equal(t, "real", r.Workers[0].Name) // Name comes from the wrapped worker
	assert.Equal(t, []string{}, r.Workers[0].QueueIDs)
}

func TestBindSkipsEmptyQueueID(t *testing.T) {
	w := newWorker(func(iJob[string]) {})
	w.bind(&inspectedQueue{MockPersistentQueue: mocks.NewMockPersistentQueue(), id: ""}, 0)
	assert.Equal(t, []string{}, w.boundQueueIDs())
}

func TestDepVersion(t *testing.T) {
	assert.Empty(t, depVersion(nil, false))
	bi := &debug.BuildInfo{Deps: []*debug.Module{{Path: "example.com/x", Version: "v9"}}}
	assert.Empty(t, depVersion(bi, true))
	bi.Deps = append(bi.Deps, &debug.Module{Path: "github.com/goptics/varmq", Version: "v1.11.0"})
	assert.Equal(t, "v1.11.0", depVersion(bi, true))
}
