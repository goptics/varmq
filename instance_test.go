package varmq

import (
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
