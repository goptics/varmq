package varmq

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goptics/varmq/internal/queues"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMemStore is an in-memory store on a fake clock the test moves.
func newMemStore(t *testing.T, opts ...StoreConfigFunc) (*memWorkflowStore, *time.Time) {
	t.Helper()
	s, ok := NewWorkflowStore(opts...).(*memWorkflowStore)
	require.True(t, ok)
	clock := time.UnixMilli(1_000_000)
	s.now = func() time.Time { return clock }

	return s, &clock
}

// submit plans wf and submits it to its store.
func submit(t *testing.T, wf *Workflow) error {
	t.Helper()
	p, err := wf.plan()
	require.NoError(t, err)

	return wf.store.Submit(context.Background(), p)
}

func end(wf, task string, o Outcome) TaskEnd {
	return TaskEnd{Workflow: wf, Task: task, Outcome: o}
}

func TestMemStoreSubmitIdempotent(t *testing.T) {
	ctx := context.Background()
	s, _ := newMemStore(t)
	assert.Equal(t, "memory:"+InstanceID, s.ScopeID())
	_, ok := any(s).(WorkflowInspector)
	assert.True(t, ok, "the store also lists workflows")

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()

	mk := func(data int) *Workflow {
		wf := NewWorkflow(s, WithWorkflowID("idem"))
		a := wf.Add("a", q, data)
		wf.Add("b", q, 2).After(a)
		return wf
	}

	require.NoError(t, submit(t, mk(1)))
	assert.Equal(t, 1, q.Len(), "root pushed")

	require.NoError(t, submit(t, mk(1)), "same plan: success")
	assert.Equal(t, 1, q.Len(), "a repeat pushes nothing")

	assert.ErrorIs(t, submit(t, mk(5)), ErrWorkflowExists, "same id, other plan")

	r, err := s.Record(ctx, "idem")
	require.NoError(t, err)
	assert.Equal(t, WorkflowRunning, r.State)

	t.Run("binds the tag to the store", func(t *testing.T) {
		v, ok := q.(*queue[int]).internalQueue.Dequeue()
		require.True(t, ok)
		j := v.(*job[int])
		require.NotNil(t, j.taskTag())
		assert.Equal(t, WorkflowStore(s), j.taskTag().store)
	})

	t.Run("durable plan refused", func(t *testing.T) {
		err := s.Submit(ctx, WorkflowPlan{ID: "dur", Hash: "h", Tasks: []TaskPlan{{Name: "a", Queue: "storage", Item: []byte("{}")}}})
		assert.ErrorIs(t, err, ErrScopeMismatch)
		_, err = s.Record(ctx, "dur")
		assert.ErrorIs(t, err, ErrWorkflowNotFound, "nothing kept")
		assert.Error(t, s.Submit(ctx, WorkflowPlan{ID: "empty", Hash: "h"}), "no tasks")
	})
}

func TestMemStoreRecord(t *testing.T) {
	ctx := context.Background()
	s, clock := newMemStore(t)

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	fifo := w.BindQueue()
	prio := w.BindPriorityQueue()
	fifoID, prioID := fifo.(*queue[int]).totals.id, prio.(*priorityQueue[int]).totals.id

	wf := NewWorkflow(s, WithWorkflowID("rec"))
	a := wf.Add("a", fifo, 1)
	b := wf.Add("b", prio, 2).After(a).Priority(2).Undo(fifo, 9)
	wf.Add("c", fifo, 3).After(a, b).Always()
	require.NoError(t, submit(t, wf))

	r, err := s.Record(ctx, "rec")
	require.NoError(t, err)
	assert.Equal(t, WorkflowRecord{
		ID: "rec", State: WorkflowRunning, CreatedAt: clock.UnixMilli(), UndoOnCancel: true,
		Tasks: []TaskRecord{
			{Name: "a", State: JobPending, QueueID: fifoID, JobID: "wf:rec:a"},
			{Name: "b", State: JobWaiting, Deps: []string{"a"}, QueueID: prioID, JobID: "wf:rec:b",
				Undo: &TaskRecord{Name: "b", State: JobWaiting, QueueID: fifoID, JobID: "wf:rec:b:undo"}},
			{Name: "c", State: JobWaiting, Deps: []string{"a", "b"}, QueueID: fifoID, JobID: "wf:rec:c", Always: true},
		},
	}, r)
	assert.Equal(t, 1, fifo.Len())

	r.Tasks[1].Deps[0] = "changed"
	r, _ = s.Record(ctx, "rec")
	assert.Equal(t, []string{"a"}, r.Tasks[1].Deps, "a record is a copy")

	e := end("rec", "a", OutcomeCompleted)
	e.Result = []byte(`{"x":1}`)
	pushed, err := s.finish(e)
	require.NoError(t, err)
	assert.Equal(t, []string{prioID}, pushed)
	assert.Equal(t, 1, prio.Len())

	e = end("rec", "b", OutcomeFailed)
	e.Reason = "boom"
	pushed, err = s.finish(e)
	require.NoError(t, err)
	assert.Equal(t, []string{fifoID}, pushed, "c is Always: runs anyway")

	r, _ = s.Record(ctx, "rec")
	assert.Equal(t, JobCompleted, r.Tasks[0].State)
	assert.Equal(t, 7, r.Tasks[0].ResultSize)
	assert.Equal(t, JobFailed, r.Tasks[1].State)
	assert.Equal(t, "boom", r.Tasks[1].Reason)
	assert.Equal(t, JobPending, r.Tasks[2].State)
	assert.Zero(t, r.FinishedAt)

	*clock = clock.Add(time.Second)
	_, err = s.finish(end("rec", "c", OutcomeCompleted))
	require.NoError(t, err)
	r, _ = s.Record(ctx, "rec")
	assert.Equal(t, WorkflowCompleted, r.State, "b's failure is absorbed by Always c")
	assert.Equal(t, clock.UnixMilli(), r.FinishedAt)

	_, err = s.Record(ctx, "nope")
	assert.ErrorIs(t, err, ErrWorkflowNotFound)
	_, err = s.finish(end("nope", "a", OutcomeCompleted))
	assert.ErrorIs(t, err, ErrWorkflowNotFound)
	_, err = s.finish(end("rec", "zzz", OutcomeCompleted))
	assert.Error(t, err, "unknown task")
}

func TestMemStoreRetention(t *testing.T) {
	ctx := context.Background()
	s, clock := newMemStore(t, WithWorkflowRetention(time.Hour))

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()
	one := func(id string, data int) *Workflow {
		wf := NewWorkflow(s, WithWorkflowID(id))
		wf.Add("a", q, data)
		return wf
	}

	require.NoError(t, submit(t, one("old", 1)))
	require.NoError(t, submit(t, one("live", 1)))
	_, err := s.finish(end("old", "a", OutcomeCompleted))
	require.NoError(t, err)

	*clock = clock.Add(59 * time.Minute)
	require.NoError(t, submit(t, one("x1", 1)))
	_, err = s.Record(ctx, "old")
	require.NoError(t, err, "inside retention")

	*clock = clock.Add(2 * time.Minute)
	require.NoError(t, submit(t, one("x2", 1)), "Submit sweeps")
	_, err = s.Record(ctx, "old")
	assert.ErrorIs(t, err, ErrWorkflowNotFound, "ended and past retention")
	_, err = s.Record(ctx, "live")
	assert.NoError(t, err, "running workflows stay")

	list, err := s.Workflows(ctx, "", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"x2", "x1", "live"}, ids(list))

	require.NoError(t, submit(t, one("old", 7)), "a swept id is free again")
}

func ids(rs []WorkflowRecord) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

func TestMemStoreWorkflows(t *testing.T) {
	ctx := context.Background()
	s, _ := newMemStore(t)

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()
	for _, id := range []string{"w1", "w2", "w3", "w4"} {
		wf := NewWorkflow(s, WithWorkflowID(id))
		wf.Add("a", q, 1)
		require.NoError(t, submit(t, wf))
	}
	_, err := s.finish(end("w2", "a", OutcomeCompleted))
	require.NoError(t, err)
	_, err = s.finish(end("w3", "a", OutcomeFailed))
	require.NoError(t, err)

	list, _ := s.Workflows(ctx, "", 0, 10)
	assert.Equal(t, []string{"w4", "w3", "w2", "w1"}, ids(list), "newest first")
	list, _ = s.Workflows(ctx, "", 1, 2)
	assert.Equal(t, []string{"w3", "w2"}, ids(list))
	list, _ = s.Workflows(ctx, WorkflowRunning, 0, 10)
	assert.Equal(t, []string{"w4", "w1"}, ids(list))
	list, _ = s.Workflows(ctx, WorkflowFailed, 0, 10)
	assert.Equal(t, []string{"w3"}, ids(list))
	list, err = s.Workflows(ctx, "", 1<<62, 1<<62)
	require.NoError(t, err)
	assert.Empty(t, list)
	assert.NotNil(t, list, "an empty page is [], not null")
}

func TestMemStoreResults(t *testing.T) {
	ctx := context.Background()
	s, _ := newMemStore(t, WithResultLimit(4))

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()
	wf := NewWorkflow(s, WithWorkflowID("res"))
	a := wf.Add("a", q, 1)
	b := wf.Add("b", q, 2)
	big := wf.Add("big", q, 3)
	none := wf.Add("none", q, 4)
	wf.Add("d", q, 5).After(a, b, big, none).Always()
	require.NoError(t, submit(t, wf))

	_, err := s.DepResult(ctx, "res", "a")
	assert.ErrorIs(t, err, ErrNoResult, "not ended yet")

	e := end("res", "a", OutcomeCompleted)
	e.Result = []byte("1234")
	_, err = s.finish(e)
	require.NoError(t, err)
	e.Result[0] = 'X' // the store keeps its own copy
	got, err := s.DepResult(ctx, "res", "a")
	require.NoError(t, err)
	assert.Equal(t, []byte("1234"), got)
	got[0] = 'Y'
	got, _ = s.DepResult(ctx, "res", "a")
	assert.Equal(t, []byte("1234"), got, "callers get a copy")

	_, err = s.finish(end("res", "b", OutcomeFailed))
	require.NoError(t, err)
	_, err = s.DepResult(ctx, "res", "b")
	assert.ErrorIs(t, err, ErrDepFailed)

	e = end("res", "big", OutcomeCompleted)
	e.Result = []byte("12345")
	_, err = s.finish(e)
	require.NoError(t, err)
	_, err = s.DepResult(ctx, "res", "big")
	assert.ErrorIs(t, err, ErrDepFailed, "over the limit: failed")
	r, _ := s.Record(ctx, "res")
	assert.Equal(t, JobFailed, r.Tasks[2].State)
	assert.Equal(t, ErrResultTooLarge.Error(), r.Tasks[2].Reason)
	assert.Zero(t, r.Tasks[2].ResultSize)

	_, err = s.finish(end("res", "none", OutcomeCompleted))
	require.NoError(t, err)
	_, err = s.DepResult(ctx, "res", "none")
	assert.ErrorIs(t, err, ErrNoResult, "completed without a result")

	_, err = s.DepResult(ctx, "res", "zzz")
	assert.ErrorIs(t, err, ErrNoResult, "not a task")
	_, err = s.DepResult(ctx, "nope", "a")
	assert.ErrorIs(t, err, ErrWorkflowNotFound)
}

func TestMemStoreCancel(t *testing.T) {
	ctx := context.Background()
	s, _ := newMemStore(t)

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()
	wf := NewWorkflow(s, WithWorkflowID("cx"))
	a := wf.Add("a", q, 1).Undo(q, 10)
	b := wf.Add("b", q, 2).After(a)
	wf.Add("c", q, 3).After(b)
	require.NoError(t, submit(t, wf))
	_, err := s.finish(end("cx", "a", OutcomeCompleted))
	require.NoError(t, err)
	// Empty q without closing: a Purge would cancel queued b.
	for q.Len() > 0 {
		q.(*queue[int]).internalQueue.Dequeue()
	}

	ok, err := s.Runnable(ctx, "cx", "b", false)
	require.NoError(t, err)
	assert.True(t, ok)

	require.NoError(t, s.Cancel(ctx, "cx", nil))
	ok, _ = s.Runnable(ctx, "cx", "b", false)
	assert.False(t, ok, "pending b is cancelled by the pre-run check")
	r, _ := s.Record(ctx, "cx")
	assert.Equal(t, JobCancelled, r.Tasks[2].State, "waiting c cancelled now")
	assert.Equal(t, WorkflowRunning, r.State, "b is still queued")

	_, err = s.finish(end("cx", "b", OutcomeCancelled))
	require.NoError(t, err)
	r, _ = s.Record(ctx, "cx")
	assert.Equal(t, WorkflowCompensating, r.State)
	assert.Equal(t, JobPending, r.Tasks[0].Undo.State)
	require.Equal(t, 1, q.Len(), "a's undo pushed")
	v, _ := q.(*queue[int]).internalQueue.Dequeue()
	assert.Equal(t, "wf:cx:a:undo", v.(*job[int]).ID())

	ok, _ = s.Runnable(ctx, "cx", "a", true)
	assert.True(t, ok)
	_, err = s.finish(TaskEnd{Workflow: "cx", Task: "a", Outcome: OutcomeCompleted, Undo: true})
	require.NoError(t, err)
	r, _ = s.Record(ctx, "cx")
	assert.Equal(t, WorkflowCompensated, r.State)
	assert.NotZero(t, r.FinishedAt)

	require.NoError(t, s.Cancel(ctx, "cx", nil), "cancel of an ended workflow: no-op")
	assert.ErrorIs(t, s.Cancel(ctx, "nope", nil), ErrWorkflowNotFound)
	_, err = s.Runnable(ctx, "nope", "a", false)
	assert.ErrorIs(t, err, ErrWorkflowNotFound)
}

// refusingQueue is custom storage with no force path that refuses adds on
// demand.
type refusingQueue struct {
	IQueue
	refuse atomic.Bool
}

func (r *refusingQueue) Enqueue(item any) bool {
	return !r.refuse.Load() && r.IQueue.Enqueue(item)
}

func (r *refusingQueue) EnqueueOnce(item any, id string) bool {
	return !r.refuse.Load() && r.IQueue.EnqueueOnce(item, id)
}

func TestMemStorePromotion(t *testing.T) {
	ctx := context.Background()

	t.Run("ignores capacity", func(t *testing.T) {
		s, _ := newMemStore(t)
		w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
		q := w.BindQueue(WithQueueCapacity(1))
		pq := w.BindPriorityQueue(WithQueueCapacity(1))

		wf := NewWorkflow(s, WithWorkflowID("cap"))
		a := wf.Add("a", q, 1)
		wf.Add("b", q, 2).After(a)
		wf.Add("c", pq, 3).After(a)
		require.NoError(t, submit(t, wf))

		_, ok := q.Add(9)
		require.False(t, ok, "root a fills q")
		_, ok = pq.Add(9, 0)
		require.True(t, ok)
		_, ok = pq.Add(10, 0)
		require.False(t, ok, "pq full")

		pushed, err := s.finish(end("cap", "a", OutcomeCompleted))
		require.NoError(t, err)
		assert.Len(t, pushed, 2)
		assert.Equal(t, 2, q.Len(), "b promoted past capacity")
		assert.Equal(t, 2, pq.Len(), "c promoted past capacity")
	})

	t.Run("refused push fails the task", func(t *testing.T) {
		s, _ := newMemStore(t)
		w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
		rq := &refusingQueue{IQueue: queues.NewQueue[iJob[int]]()}
		q := w.WithQueue(rq)

		wf := NewWorkflow(s, WithWorkflowID("ref"))
		a := wf.Add("a", q, 1)
		b := wf.Add("b", q, 2).After(a)
		wf.Add("c", q, 3).After(b)
		p, err := wf.plan()
		require.NoError(t, err)
		bJob := p.Tasks[1].Item.(*job[int])
		require.NoError(t, s.Submit(ctx, p))
		submitted := q.(*queue[int]).totals.submitted.Load()

		rq.refuse.Store(true)
		pushed, err := s.finish(end("ref", "a", OutcomeCompleted))
		require.NoError(t, err)
		assert.Empty(t, pushed)

		r, _ := s.Record(ctx, "ref")
		assert.Equal(t, JobFailed, r.Tasks[1].State)
		assert.Equal(t, "queue refused promotion", r.Tasks[1].Reason)
		assert.Equal(t, JobCancelled, r.Tasks[2].State)
		assert.Equal(t, WorkflowFailed, r.State)
		assert.True(t, bJob.IsClosed(), "the refused job is closed, so its waiters release")
		assert.Equal(t, submitted, q.(*queue[int]).totals.submitted.Load(), "a refusal is not counted")
	})

	t.Run("refused root fails at Submit", func(t *testing.T) {
		s, _ := newMemStore(t)
		w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
		rq := &refusingQueue{IQueue: queues.NewQueue[iJob[int]]()}
		rq.refuse.Store(true)
		q := w.WithQueue(rq)

		wf := NewWorkflow(s, WithWorkflowID("root"))
		wf.Add("a", q, 1)
		require.NoError(t, submit(t, wf), "stored: the refusal is the task's outcome")
		r, _ := s.Record(ctx, "root")
		assert.Equal(t, WorkflowFailed, r.State)
		assert.Equal(t, "queue refused promotion", r.Tasks[0].Reason)
	})
}

// Many workflows submitted, finished, read and swept at once: -race checks
// the one mutex; the sweep compacts the list once most of it is gone.
func TestMemStoreConcurrent(t *testing.T) {
	ctx := context.Background()
	s, _ := newMemStore(t, WithWorkflowRetention(0))

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			id := fmt.Sprintf("c%d", i)
			wf := NewWorkflow(s, WithWorkflowID(id))
			a := wf.Add("a", q, i)
			wf.Add("b", q, i).After(a)
			assert.NoError(t, submit(t, wf))
			_, err := s.finish(end(id, "a", OutcomeCompleted))
			assert.NoError(t, err)
			_, _ = s.Workflows(ctx, "", 0, 5)
			_, err = s.finish(end(id, "b", OutcomeCompleted))
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	wf := NewWorkflow(s, WithWorkflowID("last"))
	wf.Add("a", q, 0)
	require.NoError(t, submit(t, wf))
	list, _ := s.Workflows(ctx, "", 0, 100)
	assert.Equal(t, []string{"last"}, ids(list), "every ended one swept")
	assert.Len(t, s.order, 1, "compacted")
}
