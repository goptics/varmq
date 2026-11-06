package varmq

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/goptics/varmq/internal/queues"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func memOf(t *testing.T, q any) *memQueue {
	t.Helper()
	var m *memQueue
	switch h := q.(type) {
	case *queue[int]:
		m = h.totals
	case *priorityQueue[int]:
		m = h.totals
	case *errorQueue[int]:
		m = h.totals
	}
	require.NotNil(t, m, "in-memory queue has an inspector")
	return m
}

func stats(t *testing.T, m *memQueue) QueueStats {
	t.Helper()
	s, err := m.QueueStats(context.Background())
	require.NoError(t, err)
	return s
}

// An in-memory queue counts its own jobs: submitted at Add, processing while
// a job runs, completed and failed when it ends; its pending jobs are listed
// in hand-out order and it is listed under GET /queues.
func TestMemQueueCounts(t *testing.T) {
	release := make(chan struct{})
	w := NewErrWorker(func(j Job[int]) error {
		if j.Data() == 0 {
			<-release
		}
		if j.Data() == 2 {
			return errors.New("boom")
		}
		return nil
	}, 1)
	defer w.Stop()
	q := w.BindQueue()
	m := memOf(t, q)

	assert.True(t, strings.HasPrefix(m.QueueID(), "memory:"+InstanceID+":"), m.QueueID())
	got, ok := queueRegistry.Load(m.QueueID())
	require.True(t, ok, "listed under GET /queues")
	assert.Same(t, m, got)
	assert.Empty(t, w.(*errWorkerBinder[int]).boundQueueIDs(), "not an Instance Record queue id")

	q.Add(0, WithJobId("a"))
	require.Eventually(t, func() bool { return stats(t, m).Counts.Processing == 1 }, time.Second, time.Millisecond)
	q.Add(1, WithJobId("b"))
	q.Add(2)

	s := stats(t, m)
	assert.Equal(t, JobCounts{Pending: 2, Processing: 1}, s.Counts)
	assert.Equal(t, uint64(3), s.Totals.Submitted)

	jobs, err := m.Jobs(context.Background(), JobPending, 0, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 2)
	assert.Equal(t, "b", jobs[0].ID)
	assert.JSONEq(t, `1`, string(jobs[0].Payload))
	assert.Equal(t, JobPending, jobs[1].State)
	page, _ := m.Jobs(context.Background(), JobPending, 1, 1)
	assert.JSONEq(t, `2`, string(page[0].Payload), "offset and limit")
	none, _ := m.Jobs(context.Background(), JobCompleted, 0, 10)
	assert.Empty(t, none, "finished jobs are not kept")
	assert.NotNil(t, none, "an empty page, not null")

	close(release)
	require.Eventually(t, func() bool { return stats(t, m).Totals.Completed == 3 }, time.Second, time.Millisecond)
	s = stats(t, m)
	assert.Equal(t, JobCounts{}, s.Counts)
	assert.Equal(t, JobTotals{Submitted: 3, Completed: 3, Failed: 1}, s.Totals)
}

// Two unnamed workers' queues get distinct IDs.
func TestMemQueueIDsUnique(t *testing.T) {
	a := NewWorker(func(Job[int]) {})
	b := NewWorker(func(Job[int]) {})
	defer a.Stop()
	defer b.Stop()
	assert.NotEqual(t, memOf(t, a.BindQueue()).QueueID(), memOf(t, b.BindQueue()).QueueID())
}

// A priority queue lists pending jobs in the order it hands them out.
func TestMemQueuePriorityOrder(t *testing.T) {
	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	defer w.Stop()
	q := w.BindPriorityQueue()
	for _, p := range []int{5, 1, 3, 1} {
		q.Add(p*10, p)
	}
	jobs, err := memOf(t, q).Jobs(context.Background(), JobPending, 0, 10)
	require.NoError(t, err)
	var order []string
	for _, j := range jobs {
		order = append(order, string(j.Payload))
	}
	assert.Equal(t, []string{"10", "10", "30", "50"}, order)
}

// selfInspected is in-memory storage that inspects itself.
type selfInspected struct {
	*queues.Queue[any]
}

func (selfInspected) QueueID() string { return "custom:self" }
func (selfInspected) QueueStats(context.Context) (QueueStats, error) {
	return QueueStats{}, nil
}
func (selfInspected) Jobs(context.Context, JobState, int, int) ([]JobRecord, error) {
	return nil, nil
}

// Storage that is a QueueInspector keeps its own; no in-memory one is made.
func TestMemQueueSkipsSelfInspected(t *testing.T) {
	w := NewWorker(func(Job[int]) {})
	defer w.Stop()
	q := w.WithQueue(selfInspected{queues.NewQueue[any]()})
	assert.Nil(t, q.(*queue[int]).totals)
}

// Items custom storage holds are listed by their JSON, null when they have none.
func TestPendingRecord(t *testing.T) {
	assert.JSONEq(t, `{"a":1}`, string(pendingRecord(map[string]int{"a": 1}).Payload))
	assert.Equal(t, json.RawMessage("null"), pendingRecord(make(chan int)).Payload)
	r := pendingRecord(newJob("x", jobConfigs{Id: "id-1"}))
	assert.Equal(t, "id-1", r.ID)
	assert.JSONEq(t, `"x"`, string(r.Payload))
}
