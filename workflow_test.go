package varmq

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWFStore is a WorkflowStore that only names its scope.
type fakeWFStore struct{ scope string }

func (s fakeWFStore) ScopeID() string                          { return s.scope }
func (fakeWFStore) Submit(context.Context, WorkflowPlan) error { return nil }
func (fakeWFStore) Cancel(context.Context, string, bool) error { return nil }
func (fakeWFStore) Runnable(context.Context, string, string, bool) (bool, error) {
	return true, nil
}
func (fakeWFStore) DepResult(context.Context, string, string) ([]byte, error) {
	return nil, ErrNoResult
}
func (fakeWFStore) Record(context.Context, string) (WorkflowRecord, error) {
	return WorkflowRecord{}, ErrWorkflowNotFound
}

func memStore() fakeWFStore { return fakeWFStore{scope: "memory:" + InstanceID} }

// wfStorage makes a mock storage workflow-capable: scoped, inspectable,
// WorkflowAcknowledgeable.
type wfStorage struct {
	id, scope string
}

func (s wfStorage) ScopeID() string                           { return s.scope }
func (s wfStorage) QueueID() string                           { return s.id }
func (wfStorage) WorkflowStore() WorkflowStore                { return nil }
func (wfStorage) FinishTask(string, TaskEnd) ([]string, bool) { return nil, false }
func (wfStorage) QueueStats(context.Context) (QueueStats, error) {
	return QueueStats{}, nil
}
func (wfStorage) Jobs(context.Context, JobState, int, int) ([]JobRecord, error) {
	return nil, nil
}

type wfPersistent struct {
	*mocks.MockPersistentQueue
	wfStorage
}

type wfDistributedPrio struct {
	*mocks.MockDistributedPriorityQueue
	wfStorage
}

func newWFPersistent(id, scope string) wfPersistent {
	return wfPersistent{mocks.NewMockPersistentQueue(), wfStorage{id, scope}}
}

// Every handle a task can target is a TaskQueue of its data type: a
// signature drift breaks the build.
var (
	_ TaskQueue[int] = Queue[int](nil)
	_ TaskQueue[int] = PriorityQueue[int](nil)
	_ TaskQueue[int] = ErrQueue[int](nil)
	_ TaskQueue[int] = ErrPriorityQueue[int](nil)
	_ TaskQueue[int] = ResultQueue[int, string](nil)
	_ TaskQueue[int] = ResultPriorityQueue[int, string](nil)
	_ TaskQueue[int] = PersistentQueue[int](nil)
	_ TaskQueue[int] = PersistentPriorityQueue[int](nil)
	_ TaskQueue[int] = DistributedQueue[int](nil)
	_ TaskQueue[int] = DistributedPriorityQueue[int](nil)
)

func TestWorkflowBuilder(t *testing.T) {
	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	fifo := w.BindQueue()
	prio := w.BindPriorityQueue()
	ew := NewErrWorker(func(Job[string]) error { return nil }, WithAutoRun(false))
	errQ := ew.BindQueue()

	t.Run("valid chain", func(t *testing.T) {
		wf := NewWorkflow(memStore(), WithWorkflowID("order-1"), WithFailFast())
		assert.Equal(t, "order-1", wf.ID())

		a := wf.Add("a", fifo, 1)
		b := wf.Add("b", prio, 2).After(a).Priority(3)
		c := wf.Add("c", errQ, "x").After(a, b, a).Always().Undo(fifo, 9)

		p, err := wf.plan()
		require.NoError(t, err)
		assert.Equal(t, "order-1", p.ID)
		assert.True(t, p.FailFast)
		assert.True(t, p.UndoOnCancel)
		require.Len(t, p.Tasks, 3)
		assert.NotEmpty(t, p.Hash)
		_ = c

		ta, tb, tc := p.Tasks[0], p.Tasks[1], p.Tasks[2]
		assert.Equal(t, "wf:order-1:a", ta.JobID)
		assert.Empty(t, ta.Deps)
		assert.False(t, ta.HasPriority)
		assert.Equal(t, []string{"a"}, tb.Deps)
		assert.True(t, tb.HasPriority)
		assert.Equal(t, 3, tb.Priority)
		assert.Equal(t, []string{"a", "b"}, tc.Deps, "deps deduped, After order")
		assert.True(t, tc.Always)
		assert.False(t, ta.Always)

		require.NotNil(t, tc.Undo)
		assert.Equal(t, "wf:order-1:c:undo", tc.Undo.JobID)
		assert.Nil(t, ta.Undo)

		// in-memory items: the job kind the handle's worker runs, tagged
		ja, ok := ta.Item.(*job[int])
		require.True(t, ok, "fifo item %T", ta.Item)
		assert.Equal(t, "wf:order-1:a", ja.ID())
		require.NotNil(t, ja.taskTag())
		assert.Equal(t, "order-1", ja.taskTag().wf)
		assert.Equal(t, "a", ja.taskTag().task)
		_, ok = tc.Item.(iErrorJob[string])
		assert.True(t, ok, "err queue item %T", tc.Item)
		ju, ok := tc.Undo.Item.(*job[int])
		require.True(t, ok)
		assert.Equal(t, "c", ju.taskTag().task)
		assert.True(t, ju.taskTag().undo)
		assert.False(t, ja.taskTag().undo)
		assert.Equal(t, 9, ju.Data())

		// in-memory target: push stores, counts and wakes
		mt, ok := ta.Queue.(memTarget)
		require.True(t, ok, "queue %T", ta.Queue)
		assert.Equal(t, ta.QueueID, mt.queueID)
		totals := fifo.(*queue[int]).totals
		assert.Equal(t, totals.id, ta.QueueID)
		before := totals.submitted.Load()
		require.True(t, mt.push(ta.Item, 0))
		assert.Equal(t, 1, fifo.Len())
		assert.Equal(t, before+1, totals.submitted.Load())
		assert.Equal(t, "Queued", ja.Status())
		assert.Same(t, totals, ja.memTotals())
		assert.False(t, mt.push(ta.Item, 0), "same job id twice: refused")
		assert.Equal(t, before+1, totals.submitted.Load(), "refused push takes its count back")
		fifo.Purge()

		mb := tb.Queue.(memTarget)
		require.True(t, mb.push(tb.Item, 3))
		assert.Equal(t, 1, prio.Len())
		prio.Purge()
	})

	t.Run("generated id", func(t *testing.T) {
		wf := NewWorkflow(memStore())
		assert.Regexp(t, `^[A-Za-z0-9_.-]{1,128}$`, wf.ID())
		assert.NotEqual(t, wf.ID(), NewWorkflow(memStore()).ID())
		wf.Add("a", fifo, 1)
		_, err := wf.plan()
		require.NoError(t, err)
	})

	t.Run("durable targets", func(t *testing.T) {
		pw := NewWorker(func(Job[string]) {}, WithAutoRun(false))
		st := newWFPersistent("q1", "redis://x")
		pq := pw.WithPersistentQueue(st)
		dst := wfDistributedPrio{mocks.NewMockDistributedPriorityQueue(), wfStorage{"q2", "redis://x"}}
		dq := NewDistributedPriorityQueue[string](dst)

		wf := NewWorkflow(fakeWFStore{scope: "redis://x"}, WithWorkflowID("w1"), WithUndoOnCancel(false))
		a := wf.Add("a", pq, "hello")
		wf.Add("b", dq, "there").After(a).Priority(2).Undo(pq, "undo-b")

		p, err := wf.plan()
		require.NoError(t, err)
		assert.False(t, p.UndoOnCancel)

		ta, tb := p.Tasks[0], p.Tasks[1]
		assert.Equal(t, "q1", ta.QueueID)
		assert.Equal(t, st, ta.Queue)
		assert.Equal(t, "q2", tb.QueueID)
		assert.Equal(t, dst, tb.Queue)
		assert.True(t, tb.HasPriority)
		assert.Equal(t, "q1", tb.Undo.QueueID)

		var view jobView[string]
		b, ok := ta.Item.([]byte)
		require.True(t, ok, "item %T", ta.Item)
		require.NoError(t, json.Unmarshal(b, &view))
		assert.Equal(t, jobView[string]{Id: "wf:w1:a", Status: "Created", Payload: "hello",
			EnqueuedAt: view.EnqueuedAt, Wf: "w1", Task: "a"}, view)
		assert.NotZero(t, view.EnqueuedAt)

		require.NoError(t, json.Unmarshal(tb.Undo.Item.([]byte), &view))
		assert.Equal(t, "wf:w1:b:undo", view.Id)
		assert.Equal(t, "undo-b", view.Payload)
		assert.Equal(t, "b", view.Task)
		assert.True(t, view.Undo)
	})

	t.Run("every handle", func(t *testing.T) {
		rw := NewResultWorker(func(Job[int]) (int, error) { return 0, nil }, WithAutoRun(false))
		dw := NewWorker(func(Job[int]) {}, WithAutoRun(false))
		st := func(id string) wfStorage { return wfStorage{id, "redis://x"} }
		pp := struct {
			*mocks.MockPersistentPriorityQueue
			wfStorage
		}{mocks.NewMockPersistentPriorityQueue(), st("b")}
		dq := struct {
			*mocks.MockDistributedQueue
			wfStorage
		}{mocks.NewMockDistributedQueue(), st("c")}
		mem, durable := memoryScope(), "redis://x"
		isJob := func(v any) bool { _, ok := v.(*job[int]); return ok }
		isErr := func(v any) bool { _, ok := v.(iErrorJob[string]); return ok }
		isResult := func(v any) bool { _, ok := v.(iResultJob[int, int]); return ok }
		isBytes := func(v any) bool { _, ok := v.([]byte); return ok }
		item := func(it WorkflowItem, err error) WorkflowItem {
			require.NoError(t, err)
			return it
		}

		for name, c := range map[string]struct {
			item     WorkflowItem
			scope    string
			priority bool
			kind     func(any) bool
		}{
			"queue":           {item(fifo.WorkflowItem(1)), mem, false, isJob},
			"priority":        {item(prio.WorkflowItem(1)), mem, true, isJob},
			"err":             {item(errQ.WorkflowItem("x")), mem, false, isErr},
			"err priority":    {item(ew.BindPriorityQueue().WorkflowItem("x")), mem, true, isErr},
			"result":          {item(rw.BindQueue().WorkflowItem(1)), mem, false, isResult},
			"result priority": {item(rw.BindPriorityQueue().WorkflowItem(1)), mem, true, isResult},
			"persistent": {item(dw.WithPersistentQueue(newWFPersistent("a", "redis://x")).WorkflowItem(1)),
				durable, false, isBytes},
			"persistent priority": {item(dw.WithPersistentPriorityQueue(pp).WorkflowItem(1)), durable, true, isBytes},
			"distributed":         {item(NewDistributedQueue[int](dq).WorkflowItem(1)), durable, false, isBytes},
			"distributed priority": {item(NewDistributedPriorityQueue[int](wfDistributedPrio{
				mocks.NewMockDistributedPriorityQueue(), st("d")}).WorkflowItem(1)), durable, true, isBytes},
		} {
			assert.Equal(t, c.scope, c.item.target.scope, name)
			assert.Equal(t, c.priority, c.item.target.priority, name)
			assert.NotEmpty(t, c.item.target.queueID, name)
			v, err := c.item.build("j", taskTag{wf: "w", task: "t"})
			require.NoError(t, err, name)
			assert.True(t, c.kind(v), "%s: item %T", name, v)
		}
	})

	planErr := func(t *testing.T, wf *Workflow) error {
		t.Helper()
		_, err := wf.plan()
		require.Error(t, err)
		return err
	}

	t.Run("bad task name", func(t *testing.T) {
		wf := NewWorkflow(memStore())
		wf.Add("a b", fifo, 1)
		assert.ErrorContains(t, planErr(t, wf), `"a b"`)

		wf = NewWorkflow(memStore())
		wf.Add("", fifo, 1)
		planErr(t, wf)
	})

	t.Run("duplicate name", func(t *testing.T) {
		wf := NewWorkflow(memStore())
		wf.Add("a", fifo, 1)
		wf.Add("a", fifo, 2)
		assert.ErrorContains(t, planErr(t, wf), "duplicate")
	})

	t.Run("After task of another workflow", func(t *testing.T) {
		other := NewWorkflow(memStore())
		x := other.Add("x", fifo, 1)
		wf := NewWorkflow(memStore())
		wf.Add("a", fifo, 1).After(x)
		assert.ErrorContains(t, planErr(t, wf), "another workflow")

		wf = NewWorkflow(memStore())
		wf.Add("a", fifo, 1).After(nil)
		planErr(t, wf)
	})

	t.Run("After self or a later task", func(t *testing.T) {
		wf := NewWorkflow(memStore())
		a := wf.Add("a", fifo, 1)
		a.After(a)
		planErr(t, wf)

		wf = NewWorkflow(memStore())
		a = wf.Add("a", fifo, 1)
		b := wf.Add("b", fifo, 2).After(a)
		a.After(b) // would be a cycle
		planErr(t, wf)
	})

	t.Run("Priority on a FIFO queue", func(t *testing.T) {
		wf := NewWorkflow(memStore())
		wf.Add("a", fifo, 1).Priority(1)
		assert.ErrorContains(t, planErr(t, wf), "Priority")
	})

	t.Run("two scopes", func(t *testing.T) {
		pw := NewWorker(func(Job[int]) {}, WithAutoRun(false))
		pq := pw.WithPersistentQueue(newWFPersistent("q1", "redis://x"))

		wf := NewWorkflow(memStore())
		wf.Add("a", fifo, 1)
		wf.Add("b", pq, 2)
		assert.ErrorIs(t, planErr(t, wf), ErrScopeMismatch)

		// undo in another scope
		wf = NewWorkflow(memStore())
		wf.Add("a", fifo, 1).Undo(pq, 2)
		assert.ErrorIs(t, planErr(t, wf), ErrScopeMismatch)
	})

	t.Run("durable storage without workflow support", func(t *testing.T) {
		pw := NewWorker(func(Job[int]) {}, WithAutoRun(false))
		pq := pw.WithPersistentQueue(mocks.NewMockPersistentQueue())
		wf := NewWorkflow(fakeWFStore{scope: "redis://x"})
		wf.Add("a", pq, 1)
		assert.ErrorIs(t, planErr(t, wf), ErrScopeMismatch)
	})

	t.Run("nil queue", func(t *testing.T) {
		wf := NewWorkflow(memStore())
		wf.Add("a", TaskQueue[int](nil), 1)
		assert.ErrorContains(t, planErr(t, wf), "nil queue")
	})

	t.Run("call after Submit", func(t *testing.T) {
		for name, call := range map[string]func(wf *Workflow, a, b *Task){
			"Add":      func(wf *Workflow, _, _ *Task) { wf.Add("c", fifo, 3) },
			"After":    func(_ *Workflow, a, b *Task) { b.After(a) },
			"Always":   func(_ *Workflow, a, _ *Task) { a.Always() },
			"Priority": func(_ *Workflow, _, b *Task) { b.Priority(1) },
			"Undo":     func(_ *Workflow, a, _ *Task) { a.Undo(fifo, 1) },
		} {
			t.Run(name, func(t *testing.T) {
				wf := NewWorkflow(memStore())
				a := wf.Add("a", fifo, 1)
				b := wf.Add("b", prio, 2)
				_, err := wf.plan()
				require.NoError(t, err)

				wf.submitted = true
				call(wf, a, b)
				assert.ErrorIs(t, planErr(t, wf), ErrWorkflowSubmitted)
			})
		}
	})

	t.Run("workflow id", func(t *testing.T) {
		for _, id := range []string{"index", "a b", string(make([]byte, 129))} {
			wf := NewWorkflow(memStore(), WithWorkflowID(id))
			wf.Add("a", fifo, 1)
			planErr(t, wf)
		}
	})

	t.Run("no tasks", func(t *testing.T) {
		planErr(t, NewWorkflow(memStore()))
	})

	t.Run("nil store", func(t *testing.T) {
		wf := NewWorkflow(nil)
		wf.Add("a", fifo, 1)
		planErr(t, wf)
	})

	t.Run("chain errors pile up", func(t *testing.T) {
		wf := NewWorkflow(memStore())
		wf.Add("a b", fifo, 1).Priority(1)
		err := planErr(t, wf)
		assert.ErrorContains(t, err, "Priority")
		assert.ErrorContains(t, err, `"a b"`)
		assert.False(t, errors.Is(err, ErrScopeMismatch))
	})
}

func TestPlanHash(t *testing.T) {
	pw := NewWorker(func(Job[string]) {}, WithAutoRun(false))
	pq := pw.WithPersistentQueue(newWFPersistent("q1", "redis://x"))
	store := fakeWFStore{scope: "redis://x"}

	build := func(data string, opts ...WorkflowConfigFunc) WorkflowPlan {
		t.Helper()
		wf := NewWorkflow(store, append([]WorkflowConfigFunc{WithWorkflowID("w")}, opts...)...)
		a := wf.Add("a", pq, data)
		wf.Add("b", pq, "b").After(a).Undo(pq, "u")
		p, err := wf.plan()
		require.NoError(t, err)
		return p
	}

	p1 := build("x")
	time.Sleep(2 * time.Millisecond) // a new enqueued_at
	p2 := build("x")
	assert.NotEqual(t, p1.Tasks[0].Item, p2.Tasks[0].Item, "items differ in enqueued_at")
	assert.Equal(t, p1.Hash, p2.Hash, "enqueued_at is not in the hash")
	assert.Regexp(t, `^[0-9a-f]{64}$`, p1.Hash)

	assert.NotEqual(t, p1.Hash, build("y").Hash, "data")
	assert.NotEqual(t, p1.Hash, build("x", WithFailFast()).Hash, "FailFast")
	assert.NotEqual(t, p1.Hash, build("x", WithUndoOnCancel(false)).Hash, "UndoOnCancel")

	// undo data and Always are in it too
	wf := NewWorkflow(store, WithWorkflowID("w"))
	a := wf.Add("a", pq, "x")
	wf.Add("b", pq, "b").After(a).Undo(pq, "v")
	p3, err := wf.plan()
	require.NoError(t, err)
	assert.NotEqual(t, p1.Hash, p3.Hash, "undo data")

	wf = NewWorkflow(store, WithWorkflowID("w"))
	a = wf.Add("a", pq, "x")
	wf.Add("b", pq, "b").After(a).Undo(pq, "u").Always()
	p4, err := wf.plan()
	require.NoError(t, err)
	assert.NotEqual(t, p1.Hash, p4.Hash, "Always")
}

func TestWorkflowHandleOps(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	w := NewWorker(func(j Job[int]) {
		if j.Data() == 1 {
			started <- struct{}{}
			<-release
		}
	})
	q := w.BindQueue()
	store := NewWorkflowStore()

	build := func(bData int) *Workflow {
		wf := NewWorkflow(store, WithWorkflowID("h1"))
		a := wf.Add("a", q, 1)
		wf.Add("b", q, bData).After(a)
		return wf
	}

	wf := build(2)
	require.NoError(t, wf.Submit(ctx))
	require.NoError(t, wf.Submit(ctx), "same shape: idempotent")
	assert.ErrorIs(t, build(3).Submit(ctx), ErrWorkflowExists)

	<-started
	h := OpenWorkflow(store, "h1")
	require.NoError(t, h.Cancel(ctx))
	close(release) // a completes through its worker; waiting b was cancelled

	r, err := wf.Wait(ctx)
	require.NoError(t, err)
	assert.Equal(t, WorkflowCancelled, r.State)
	assert.Equal(t, JobCompleted, r.Tasks[0].State, "a's worker reported it")
	st, err := h.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, WorkflowCancelled, st.State)

	h.Add("c", q, 1)
	assert.ErrorIs(t, h.Submit(ctx), ErrWorkflowSubmitted)

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = wf.Wait(cctx)
	assert.ErrorIs(t, err, context.Canceled)

	_, err = OpenWorkflow(store, "nope").Status(ctx)
	assert.ErrorIs(t, err, ErrWorkflowNotFound)
}
