package varmq

import (
	"context"
	"encoding/json"
	"errors"
	"math"
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
	}, 1, WithName("memq-counts"))
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
	past, err := m.Jobs(context.Background(), JobPending, math.MaxInt, 1000)
	require.NoError(t, err, "no offset+limit overflow")
	assert.Empty(t, past)
	none, _ := m.Jobs(context.Background(), JobCompleted, 0, 10)
	assert.Empty(t, none, "finished jobs are not kept")
	assert.NotNil(t, none, "an empty page, not null")

	close(release)
	require.Eventually(t, func() bool { s := stats(t, m); return s.Totals.Completed+s.Totals.Failed == 3 }, time.Second, time.Millisecond)
	s = stats(t, m)
	assert.Equal(t, JobCounts{}, s.Counts)
	assert.Equal(t, JobTotals{Submitted: 3, Completed: 2, Failed: 1}, s.Totals, "completed counts successful jobs, as redisq")
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

// GET /queues lists a memory queue while its worker is listed under GET
// /workers: never for an unnamed worker, until the stopped retention for a
// named one.
func TestMemQueueListedWithItsWorker(t *testing.T) {
	anon := NewWorker(func(Job[int]) {})
	defer anon.Stop()
	_, listed := queueRegistry.Load(memOf(t, anon.BindQueue()).QueueID())
	assert.False(t, listed, "unnamed worker: not listed")

	w := NewWorker(func(Job[int]) {}, WithName("memq-retention"), WithStoppedRetention(20*time.Millisecond))
	id := memOf(t, w.BindQueue()).QueueID()
	_, listed = queueRegistry.Load(id)
	assert.True(t, listed)

	require.NoError(t, w.Stop())
	require.Eventually(t, func() bool { _, ok := queueRegistry.Load(id); return !ok }, time.Second, time.Millisecond, "gone after retention")
	require.NoError(t, w.Start())
	_, listed = queueRegistry.Load(id)
	assert.True(t, listed, "back on restart")
	require.NoError(t, w.Stop())
}

// Jobs a worker took in a batch but has not started count as processing, so
// pending + processing + completed + failed adds up to submitted.
func TestMemQueueCountsBatchedJobs(t *testing.T) {
	release := make(chan struct{})
	w := NewWorker(func(j Job[int]) {
		if j.Data() == 0 {
			<-release
		}
	}, 1)
	defer w.Stop()
	w.(*workerBinder[int]).jobNanos.Store(1) // measured: cheap jobs batch
	q := w.BindQueue()
	m := memOf(t, q)

	q.Add(0)
	require.Eventually(t, func() bool { return stats(t, m).Counts.Processing == 1 }, time.Second, time.Millisecond)
	for i := 1; i <= 100; i++ {
		q.Add(i)
	}
	s := stats(t, m)
	assert.EqualValues(t, 101, s.Totals.Submitted)
	assert.EqualValues(t, 101, s.Counts.Pending+s.Counts.Processing, "every job pending or processing")

	close(release)
	require.Eventually(t, func() bool { return stats(t, m).Totals.Completed == 101 }, time.Second, time.Millisecond)
	assert.Equal(t, JobCounts{}, stats(t, m).Counts)
}

// A worker that takes over another's name unlists the other's memory queues.
func TestMemQueueNameTakeover(t *testing.T) {
	a := NewWorker(func(Job[int]) {}, WithName("memq-takeover"))
	defer a.Stop()
	idA := memOf(t, a.BindQueue()).QueueID()
	_, listed := queueRegistry.Load(idA)
	require.True(t, listed)

	b := NewWorker(func(Job[int]) {}, WithName("memq-takeover"))
	defer b.Stop()
	idB := memOf(t, b.BindQueue()).QueueID()
	_, listed = queueRegistry.Load(idA)
	assert.False(t, listed, "a's queue left with a's name")
	_, listed = queueRegistry.Load(idB)
	assert.True(t, listed)
}

// Acknowledging storage keeps its own counts: no in-memory inspector.
func TestMemQueueSkipsAckStorage(t *testing.T) {
	w := NewWorker(func(Job[int]) {})
	defer w.Stop()
	q := w.WithQueue(ackMemQueue{queues.NewQueue[any]()})
	assert.Nil(t, q.(*queue[int]).totals)
}

type ackMemQueue struct{ *queues.Queue[any] }

func (ackMemQueue) Acknowledge(string) bool               { return true }
func (ackMemQueue) Reject(string, string) bool            { return true }
func (ackMemQueue) DequeueWithAckId() (any, bool, string) { return nil, false, "" }

// A foreign value under the name is replaced without a panic.
func TestMemQueueForeignRegistryEntry(t *testing.T) {
	WorkerRegistry.Store("memq-foreign", "not a worker")
	w := NewWorker(func(Job[int]) {}, WithName("memq-foreign"))
	defer w.Stop()
	_, listed := queueRegistry.Load(memOf(t, w.BindQueue()).QueueID())
	assert.True(t, listed)
}

// shrinking reports more items than Values returns, as a queue a worker
// drains between the two calls.
type shrinking struct{ *queues.Queue[any] }

func (shrinking) Len() int { return 5 }

// A page past what Values returned is empty, even when Len said more.
func TestMemQueueJobsQueueShrank(t *testing.T) {
	m := &memQueue{q: shrinking{queues.NewQueue[any]()}}
	jobs, err := m.Jobs(context.Background(), JobPending, 2, 10)
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

// Jobs taken in a batch and then closed by the user, or purged, before they
// start leave processing: the count returns to 0.
func TestMemQueueBatchedJobsClosedOrPurged(t *testing.T) {
	for _, purge := range []bool{false, true} {
		release := make(chan struct{})
		w := NewWorker(func(j Job[int]) {
			if j.Data() == 0 {
				<-release
			}
		}, 1)
		q := w.BindQueue()
		m := memOf(t, q)

		require.NoError(t, w.Pause())
		var jobs []EnqueuedJob
		for i := range 4 {
			j, ok := q.Add(i)
			require.True(t, ok)
			jobs = append(jobs, j)
		}
		require.NoError(t, w.Resume()) // concurrency 1: one batch of all 4
		require.Eventually(t, func() bool { return stats(t, m).Counts.Processing == 4 }, time.Second, time.Millisecond)

		if purge {
			q.Purge() // the rest of the batch goes stale
		} else {
			require.NoError(t, jobs[2].Close()) // skipped when its turn comes
		}
		close(release)
		require.Eventually(t, func() bool { return stats(t, m).Counts.Processing == 0 }, time.Second, time.Millisecond, "purge=%v", purge)
		w.Stop()
	}
}
