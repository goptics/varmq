package varmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"weak"

	"github.com/goptics/varmq/internal/queues"
	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWFStore is a WorkflowStore that only names its scope.
type fakeWFStore struct{ scope string }

func (s fakeWFStore) ScopeID() string                           { return s.scope }
func (fakeWFStore) Submit(context.Context, WorkflowPlan) error  { return nil }
func (fakeWFStore) Cancel(context.Context, string, *bool) error { return nil }
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
func (s wfStorage) WorkflowStore() WorkflowStore              { return fakeWFStore{scope: s.scope} }
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

// noStorePersistent is workflow storage whose WorkflowStore is nil.
type noStorePersistent struct{ wfPersistent }

func (noStorePersistent) WorkflowStore() WorkflowStore { return nil }

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

	t.Run("durable storage with no workflow store", func(t *testing.T) {
		pw := NewWorker(func(Job[int]) {}, WithAutoRun(false))
		pq := pw.WithPersistentQueue(noStorePersistent{newWFPersistent("q1", "redis://x")})
		wf := NewWorkflow(fakeWFStore{scope: "redis://x"})
		wf.Add("a", pq, 1)
		err := planErr(t, wf)
		assert.ErrorIs(t, err, ErrScopeMismatch)
		assert.ErrorContains(t, err, "storage has no workflow store")
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

// pokeStorage is durable storage that finishes tasks and wakes nobody on a
// push, as a non-distributed redisq queue.
type pokeStorage struct {
	*mocks.MockPersistentQueue
	wfStorage
}

func (pokeStorage) FinishTask(string, TaskEnd) ([]string, bool) { return nil, true }

// pushStore pushes roots at Submit and every undo at Cancel straight into
// their storage, as a store's script does, leaving the wake to varmq.
type pushStore struct {
	fakeWFStore
	plan WorkflowPlan
}

func (s *pushStore) Submit(_ context.Context, p WorkflowPlan) error {
	s.plan = p
	for _, tp := range p.Tasks {
		if len(tp.Deps) == 0 {
			tp.Queue.(pokeStorage).Enqueue(tp.Item)
		}
	}
	return nil
}

func (s *pushStore) Cancel(context.Context, string, *bool) error {
	for _, tp := range s.plan.Tasks {
		if tp.Undo != nil {
			tp.Undo.Queue.(pokeStorage).Enqueue(tp.Undo.Item)
		}
	}
	return nil
}

func (s *pushStore) Record(context.Context, string) (WorkflowRecord, error) {
	r := WorkflowRecord{ID: s.plan.ID, State: WorkflowRunning}
	for _, tp := range s.plan.Tasks {
		tr := TaskRecord{Name: tp.Name, QueueID: tp.QueueID}
		if tp.Undo != nil {
			tr.Undo = &TaskRecord{Name: tp.Name, QueueID: tp.Undo.QueueID}
		}
		r.Tasks = append(r.Tasks, tr)
	}
	return r, nil
}

// TestWorkflowPokesLocal: Submit wakes local consumers of the roots' queues,
// Cancel those of the undo queues (from the plan, or the record for an
// opened handle): durable storage wakes only distributed ones.
func TestWorkflowPokesLocal(t *testing.T) {
	for _, opened := range []bool{false, true} {
		t.Run(fmt.Sprint("opened=", opened), func(t *testing.T) {
			ctx := context.Background()
			ran := make(chan int, 4)
			run := func(j Job[int]) { ran <- j.Data() }
			tq := pokeStorage{mocks.NewMockPersistentQueue(), wfStorage{"q-t", "s"}}
			uq := pokeStorage{mocks.NewMockPersistentQueue(), wfStorage{"q-u", "s"}}
			tw := NewWorker(run)
			th := tw.WithPersistentQueue(tq)
			uw := NewWorker(run)
			uh := uw.WithPersistentQueue(uq)
			tw.WaitUntilIdle()
			uw.WaitUntilIdle()

			store := &pushStore{fakeWFStore: fakeWFStore{scope: "s"}}
			wf := NewWorkflow(store, WithWorkflowID("pk"))
			wf.Add("a", th, 1).Undo(uh, 10)
			require.NoError(t, wf.Submit(ctx))
			select {
			case v := <-ran:
				assert.Equal(t, 1, v)
			case <-time.After(2 * time.Second):
				t.Fatal("root never ran: no local poke after Submit")
			}

			h := wf
			if opened {
				h = OpenWorkflow(store, "pk")
			}
			require.NoError(t, h.Cancel(ctx))
			select {
			case v := <-ran:
				assert.Equal(t, 10, v)
			case <-time.After(2 * time.Second):
				t.Fatal("undo never ran: no local poke after Cancel")
			}
		})
	}
}

// lostReplyStore commits the Submit (pushes the roots), then fails it, as
// a store whose reply was lost.
type lostReplyStore struct{ *pushStore }

func (s lostReplyStore) Submit(ctx context.Context, p WorkflowPlan) error {
	_ = s.pushStore.Submit(ctx, p)
	return errors.New("reply lost")
}

// TestWorkflowPokesLocalOnSubmitError: a failed Submit still wakes the
// roots' local consumers; the store may have committed.
func TestWorkflowPokesLocalOnSubmitError(t *testing.T) {
	ran := make(chan int, 1)
	tq := pokeStorage{mocks.NewMockPersistentQueue(), wfStorage{"q-t", "s"}}
	tw := NewWorker(func(j Job[int]) { ran <- j.Data() })
	th := tw.WithPersistentQueue(tq)
	tw.WaitUntilIdle()

	wf := NewWorkflow(lostReplyStore{&pushStore{fakeWFStore: fakeWFStore{scope: "s"}}}, WithWorkflowID("lr"))
	wf.Add("a", th, 1)
	require.EqualError(t, wf.Submit(context.Background()), "reply lost")
	assert.False(t, wf.submitted, "retryable")
	select {
	case v := <-ran:
		assert.Equal(t, 1, v)
	case <-time.After(2 * time.Second):
		t.Fatal("root never ran: no local poke after a failed Submit")
	}
}

// zeroQueue is a user TaskQueue that returns a zero WorkflowItem.
type zeroQueue struct{}

func (zeroQueue) WorkflowItem(int) (WorkflowItem, error) { return WorkflowItem{}, nil }

// TestWorkflowGuards: misuse returns errors, never panics.
func TestWorkflowGuards(t *testing.T) {
	ctx := context.Background()

	t.Run("zero WorkflowItem", func(t *testing.T) {
		for _, store := range []WorkflowStore{nil, memStore()} {
			wf := NewWorkflow(store, WithWorkflowID("z"))
			wf.Add("a", zeroQueue{}, 1).Undo(zeroQueue{}, 2)
			err := wf.Submit(ctx)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no workflow item")
		}
	})

	t.Run("nil store", func(t *testing.T) {
		for _, wf := range []*Workflow{NewWorkflow(nil), OpenWorkflow(nil, "x")} {
			assert.ErrorIs(t, wf.Cancel(ctx), errNoWorkflowStore)
			_, err := wf.Status(ctx)
			assert.ErrorIs(t, err, errNoWorkflowStore)
			_, err = wf.Wait(ctx)
			assert.ErrorIs(t, err, errNoWorkflowStore)
		}
	})

	t.Run("invalid OpenWorkflow id", func(t *testing.T) {
		for _, id := range []string{"", "index", "a b", "a:b", strings.Repeat("x", 129)} {
			h := OpenWorkflow(panicStore{}, id)
			assert.ErrorIs(t, h.Cancel(ctx), ErrWorkflowNotFound, id)
			_, err := h.Status(ctx)
			assert.ErrorIs(t, err, ErrWorkflowNotFound, id)
			_, err = h.Wait(ctx)
			assert.ErrorIs(t, err, ErrWorkflowNotFound, id)
		}
	})
}

// panicStore fails the test if any op reaches it.
type panicStore struct{ fakeWFStore }

func (panicStore) Cancel(context.Context, string, *bool) error { panic("store asked") }
func (panicStore) Record(context.Context, string) (WorkflowRecord, error) {
	panic("store asked")
}

// mtask builds a TaskPlan for machine tests.
type mtask struct {
	name   string
	deps   []string
	always bool
	undo   bool
}

func mplan(failFast bool, ts ...mtask) WorkflowPlan {
	p := WorkflowPlan{ID: "wf", FailFast: failFast}
	for _, t := range ts {
		tp := TaskPlan{Name: t.name, Deps: t.deps, Always: t.always}
		if t.undo {
			tp.Undo = &UndoPlan{}
		}
		p.Tasks = append(p.Tasks, tp)
	}
	return p
}

// mstep is one input: a finish, or a cancel when cancel is set.
type mstep struct {
	task    string
	out     Outcome
	undo    bool
	cancel  bool
	cundo   bool
	effects []string // "B", "B:undo", "end:completed"
}

func fin(task string, out Outcome, effects ...string) mstep {
	return mstep{task: task, out: out, effects: effects}
}

func finUndo(task string, out Outcome, effects ...string) mstep {
	return mstep{task: task, out: out, undo: true, effects: effects}
}

func cancelStep(undo bool, effects ...string) mstep {
	return mstep{cancel: true, cundo: undo, effects: effects}
}

func fxStrings(fx []wfEffect) []string {
	out := []string{}
	for _, e := range fx {
		switch {
		case e.Push != "" && e.Undo:
			out = append(out, e.Push+":undo")
		case e.Push != "":
			out = append(out, e.Push)
		}
		if e.End != "" {
			out = append(out, "end:"+string(e.End))
		}
	}
	return out
}

func TestMachine(t *testing.T) {
	const (
		ok   = OutcomeCompleted
		bad  = OutcomeFailed
		gone = OutcomeCancelled
	)
	cases := []struct {
		name   string
		plan   WorkflowPlan
		roots  []string
		steps  []mstep
		final  WorkflowState
		states map[string]JobState
		undos  map[string]JobState
	}{
		{
			name:  "chain completed",
			plan:  mplan(false, mtask{name: "A"}, mtask{name: "B", deps: []string{"A"}}, mtask{name: "C", deps: []string{"B"}}),
			roots: []string{"A"},
			steps: []mstep{fin("A", ok, "B"), fin("B", ok, "C"), fin("C", ok, "end:completed")},
			final: WorkflowCompleted,
			states: map[string]JobState{
				"A": JobCompleted, "B": JobCompleted, "C": JobCompleted,
			},
		},
		{
			name: "fan-out pushes in Add order, fan-in waits for both",
			plan: mplan(false,
				mtask{name: "A"},
				mtask{name: "C", deps: []string{"A"}},
				mtask{name: "B", deps: []string{"A"}},
				mtask{name: "D", deps: []string{"B", "C"}},
			),
			roots: []string{"A"},
			steps: []mstep{fin("A", ok, "C", "B"), fin("B", ok), fin("C", ok, "D"), fin("D", ok, "end:completed")},
			final: WorkflowCompleted,
		},
		{
			name:  "two roots in Add order",
			plan:  mplan(false, mtask{name: "Y"}, mtask{name: "X"}),
			roots: []string{"Y", "X"},
			steps: []mstep{fin("X", ok), fin("Y", ok, "end:completed")},
			final: WorkflowCompleted,
		},
		{
			name: "diamond, B failed, D Always: absorbed",
			plan: mplan(false,
				mtask{name: "A"},
				mtask{name: "B", deps: []string{"A"}},
				mtask{name: "C", deps: []string{"A"}},
				mtask{name: "D", deps: []string{"B", "C"}, always: true},
			),
			roots: []string{"A"},
			steps: []mstep{fin("A", ok, "B", "C"), fin("B", bad), fin("C", ok, "D"), fin("D", ok, "end:completed")},
			final: WorkflowCompleted,
			states: map[string]JobState{
				"A": JobCompleted, "B": JobFailed, "C": JobCompleted, "D": JobCompleted,
			},
		},
		{
			name: "diamond, B failed, D not Always: cancelled, failed",
			plan: mplan(false,
				mtask{name: "A"},
				mtask{name: "B", deps: []string{"A"}},
				mtask{name: "C", deps: []string{"A"}},
				mtask{name: "D", deps: []string{"B", "C"}},
			),
			roots: []string{"A"},
			steps: []mstep{fin("A", ok, "B", "C"), fin("B", bad), fin("C", ok, "end:failed")},
			final: WorkflowFailed,
			states: map[string]JobState{
				"A": JobCompleted, "B": JobFailed, "C": JobCompleted, "D": JobCancelled,
			},
		},
		{
			name: "cancel recursion: C Always after cancelled B, not an extra failure",
			plan: mplan(false,
				mtask{name: "A"},
				mtask{name: "B", deps: []string{"A"}},
				mtask{name: "C", deps: []string{"B"}, always: true},
			),
			roots:  []string{"A"},
			steps:  []mstep{fin("A", bad, "C"), fin("C", ok, "end:failed")},
			final:  WorkflowFailed,
			states: map[string]JobState{"A": JobFailed, "B": JobCancelled, "C": JobCompleted},
		},
		{
			name:   "failed with no dependents",
			plan:   mplan(false, mtask{name: "A"}, mtask{name: "B", deps: []string{"A"}}),
			roots:  []string{"A"},
			steps:  []mstep{fin("A", ok, "B"), fin("B", bad, "end:failed")},
			final:  WorkflowFailed,
			states: map[string]JobState{"A": JobCompleted, "B": JobFailed},
		},
		{
			name: "undo in reverse DAG, compensated",
			plan: mplan(false,
				mtask{name: "A", undo: true},
				mtask{name: "B", deps: []string{"A"}, undo: true},
				mtask{name: "C", deps: []string{"B"}},
			),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B"), fin("B", ok, "C"),
				fin("C", bad, "B:undo"),
				finUndo("B", ok, "A:undo"),
				finUndo("A", ok, "end:compensated"),
			},
			final:  WorkflowCompensated,
			states: map[string]JobState{"A": JobCompleted, "B": JobCompleted, "C": JobFailed},
			undos:  map[string]JobState{"A": JobCompleted, "B": JobCompleted, "C": ""},
		},
		{
			name: "undo fails, others still run, compensation_failed",
			plan: mplan(false,
				mtask{name: "A", undo: true},
				mtask{name: "B", deps: []string{"A"}, undo: true},
				mtask{name: "C", deps: []string{"B"}},
			),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B"), fin("B", ok, "C"),
				fin("C", bad, "B:undo"),
				finUndo("B", bad, "A:undo"),
				finUndo("A", ok, "end:compensation_failed"),
			},
			final: WorkflowCompensationFailed,
			undos: map[string]JobState{"A": JobCompleted, "B": JobFailed},
		},
		{
			name: "undo with no completed descendant undo runs at once",
			plan: mplan(false,
				mtask{name: "A", undo: true},
				mtask{name: "B", deps: []string{"A"}},
				mtask{name: "C", deps: []string{"B"}},
			),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B"), fin("B", ok, "C"),
				fin("C", bad, "A:undo"),
				finUndo("A", ok, "end:compensated"),
			},
			final: WorkflowCompensated,
		},
		{
			name: "undo of a task whose dependency has no undo",
			plan: mplan(false,
				mtask{name: "A"},
				mtask{name: "B", deps: []string{"A"}, undo: true},
				mtask{name: "C", deps: []string{"B"}},
			),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B"), fin("B", ok, "C"),
				fin("C", bad, "B:undo"),
				finUndo("B", gone, "end:compensation_failed"),
			},
			final: WorkflowCompensationFailed,
			undos: map[string]JobState{"A": "", "B": JobFailed},
		},
		{
			name: "saga order looks through a task without undo",
			plan: mplan(false,
				mtask{name: "A", undo: true},
				mtask{name: "B", deps: []string{"A"}},
				mtask{name: "C", deps: []string{"B"}, undo: true},
				mtask{name: "D", deps: []string{"C"}},
			),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B"), fin("B", ok, "C"), fin("C", ok, "D"),
				fin("D", bad, "C:undo"),
				finUndo("C", ok, "A:undo"),
				finUndo("A", ok, "end:compensated"),
			},
			final: WorkflowCompensated,
			undos: map[string]JobState{"A": JobCompleted, "B": "", "C": JobCompleted, "D": ""},
		},
		{
			name: "saga order waits for descendants past a failed branch",
			plan: mplan(false,
				mtask{name: "A", undo: true},
				mtask{name: "B", deps: []string{"A"}},
				mtask{name: "C", deps: []string{"B"}, always: true, undo: true},
				mtask{name: "F", deps: []string{"C"}},
			),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B"), fin("B", bad, "C"), fin("C", ok, "F"),
				fin("F", bad, "C:undo"),
				finUndo("C", ok, "A:undo"),
				finUndo("A", ok, "end:compensated"),
			},
			final: WorkflowCompensated,
		},
		{
			name: "fan-in undo waits for every completed dependent's undo",
			plan: mplan(false,
				mtask{name: "A", undo: true},
				mtask{name: "B", deps: []string{"A"}, undo: true},
				mtask{name: "C", deps: []string{"A"}, undo: true},
				mtask{name: "D", deps: []string{"B", "C"}},
			),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B", "C"), fin("B", ok), fin("C", ok, "D"),
				fin("D", bad, "B:undo", "C:undo"),
				finUndo("C", ok),
				finUndo("B", ok, "A:undo"),
				finUndo("A", ok, "end:compensated"),
			},
			final: WorkflowCompensated,
		},
		{
			name: "undo only for completed tasks",
			plan: mplan(false,
				mtask{name: "A", undo: true},
				mtask{name: "B", deps: []string{"A"}, undo: true},
			),
			roots: []string{"A"},
			steps: []mstep{fin("A", ok, "B"), fin("B", bad, "A:undo"), finUndo("A", ok, "end:compensated")},
			final: WorkflowCompensated,
			undos: map[string]JobState{"A": JobCompleted, "B": ""},
		},
		{
			name: "FailFast cancels waiting at once",
			plan: mplan(true,
				mtask{name: "A"}, mtask{name: "B", deps: []string{"A"}},
				mtask{name: "X"}, mtask{name: "Y", deps: []string{"X"}},
			),
			roots: []string{"A", "X"},
			steps: []mstep{fin("A", bad), fin("X", ok, "end:failed")},
			final: WorkflowFailed,
			states: map[string]JobState{
				"A": JobFailed, "B": JobCancelled, "X": JobCompleted, "Y": JobCancelled,
			},
		},
		{
			name: "no FailFast lets unrelated branches finish",
			plan: mplan(false,
				mtask{name: "A"}, mtask{name: "B", deps: []string{"A"}},
				mtask{name: "X"}, mtask{name: "Y", deps: []string{"X"}},
			),
			roots: []string{"A", "X"},
			steps: []mstep{fin("A", bad), fin("X", ok, "Y"), fin("Y", ok, "end:failed")},
			final: WorkflowFailed,
			states: map[string]JobState{
				"A": JobFailed, "B": JobCancelled, "X": JobCompleted, "Y": JobCompleted,
			},
		},
		{
			// A's unabsorbed failure fires FailFast (E cancelled, D kept).
			// Q's later failure must not fire it again and cancel D.
			name: "FailFast fires on the first unabsorbed failure only",
			plan: mplan(true,
				mtask{name: "A"},
				mtask{name: "P"},
				mtask{name: "Q"},
				mtask{name: "D", deps: []string{"A", "P"}, always: true},
				mtask{name: "E", deps: []string{"A"}},
			),
			roots: []string{"A", "P", "Q"},
			steps: []mstep{fin("A", bad), fin("Q", bad), fin("P", ok, "D"), fin("D", ok, "end:failed")},
			final: WorkflowFailed,
			states: map[string]JobState{
				"A": JobFailed, "P": JobCompleted, "Q": JobFailed, "D": JobCompleted, "E": JobCancelled,
			},
		},
		{
			name:  "FailFast ignores absorbed failure",
			plan:  mplan(true, mtask{name: "A"}, mtask{name: "B", deps: []string{"A"}, always: true}, mtask{name: "X"}, mtask{name: "Y", deps: []string{"X"}}),
			roots: []string{"A", "X"},
			steps: []mstep{fin("A", bad, "B"), fin("X", ok, "Y"), fin("B", ok), fin("Y", ok, "end:completed")},
			final: WorkflowCompleted,
			states: map[string]JobState{
				"A": JobFailed, "B": JobCompleted, "X": JobCompleted, "Y": JobCompleted,
			},
		},
		{
			// D is A's Always dependent: kept, and still counts W, which
			// FailFast cancelled. E (A's other dependent) and Y are cancelled.
			name: "FailFast keeps the failed task's Always dependents",
			plan: mplan(true,
				mtask{name: "A"},
				mtask{name: "X"},
				mtask{name: "W", deps: []string{"X"}},
				mtask{name: "D", deps: []string{"A", "X", "W"}, always: true},
				mtask{name: "E", deps: []string{"A"}},
				mtask{name: "Y", deps: []string{"X"}},
			),
			roots: []string{"A", "X"},
			steps: []mstep{fin("A", bad), fin("X", ok, "D"), fin("D", ok, "end:failed")},
			final: WorkflowFailed,
			states: map[string]JobState{
				"A": JobFailed, "X": JobCompleted, "W": JobCancelled,
				"D": JobCompleted, "E": JobCancelled, "Y": JobCancelled,
			},
		},
		{
			name:   "cancel without undo: pending stays until it finishes",
			plan:   mplan(false, mtask{name: "A", undo: true}, mtask{name: "B", deps: []string{"A"}}),
			roots:  []string{"A"},
			steps:  []mstep{fin("A", ok, "B"), cancelStep(false), fin("B", ok, "end:cancelled")},
			final:  WorkflowCancelled,
			states: map[string]JobState{"A": JobCompleted, "B": JobCompleted},
			undos:  map[string]JobState{"A": ""},
		},
		{
			name:  "cancel waiting task",
			plan:  mplan(false, mtask{name: "A", undo: true}, mtask{name: "B", deps: []string{"A"}}, mtask{name: "C", deps: []string{"B"}}),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B"),
				cancelStep(false),
				fin("B", gone, "end:cancelled"),
			},
			final:  WorkflowCancelled,
			states: map[string]JobState{"A": JobCompleted, "B": JobCancelled, "C": JobCancelled},
		},
		{
			name:  "cancel with undo compensates",
			plan:  mplan(false, mtask{name: "A", undo: true}, mtask{name: "B", deps: []string{"A"}}, mtask{name: "C", deps: []string{"B"}}),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B"),
				cancelStep(true),
				fin("B", gone, "A:undo"),
				finUndo("A", ok, "end:compensated"),
			},
			final:  WorkflowCompensated,
			states: map[string]JobState{"A": JobCompleted, "B": JobCancelled, "C": JobCancelled},
		},
		{
			name: "cancel with undo after an Always dependent completed",
			plan: mplan(false,
				mtask{name: "A", undo: true},
				mtask{name: "B", deps: []string{"A"}, always: true},
				mtask{name: "X"},
			),
			roots: []string{"A", "X"},
			steps: []mstep{
				fin("A", ok, "B"), fin("B", ok),
				cancelStep(true),
				fin("X", gone, "A:undo"),
				finUndo("A", ok, "end:compensated"),
			},
			final: WorkflowCompensated,
		},
		{
			name:  "cancel with undo, no undo to run: cancelled",
			plan:  mplan(false, mtask{name: "A"}, mtask{name: "B", deps: []string{"A"}}),
			roots: []string{"A"},
			steps: []mstep{fin("A", ok, "B"), cancelStep(true), fin("B", gone, "end:cancelled")},
			final: WorkflowCancelled,
		},
		{
			name:  "cancel on ended workflow: no effects",
			plan:  mplan(false, mtask{name: "A"}),
			roots: []string{"A"},
			steps: []mstep{fin("A", ok, "end:completed"), cancelStep(true), cancelStep(false)},
			final: WorkflowCompleted,
		},
		{
			name:  "second cancel: no effects",
			plan:  mplan(false, mtask{name: "A"}),
			roots: []string{"A"},
			steps: []mstep{cancelStep(false), cancelStep(true), fin("A", gone, "end:cancelled")},
			final: WorkflowCancelled,
		},
		{
			name: "cancel after a failure keeps its compensation",
			plan: mplan(false,
				mtask{name: "A", undo: true},
				mtask{name: "B", deps: []string{"A"}},
				mtask{name: "C", deps: []string{"A"}},
				mtask{name: "D", deps: []string{"C"}},
			),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B", "C"), fin("B", bad),
				cancelStep(false),
				fin("C", gone, "A:undo"),
				finUndo("A", ok, "end:compensated"),
			},
			final:  WorkflowCompensated,
			states: map[string]JobState{"B": JobFailed, "C": JobCancelled, "D": JobCancelled},
		},
		{
			name: "cancel after a failure, no undo: failed",
			plan: mplan(false,
				mtask{name: "A"},
				mtask{name: "B", deps: []string{"A"}},
				mtask{name: "C", deps: []string{"A"}},
				mtask{name: "D", deps: []string{"C"}},
			),
			roots: []string{"A"},
			steps: []mstep{fin("A", ok, "B", "C"), fin("B", bad), cancelStep(true), fin("C", gone, "end:failed")},
			final: WorkflowFailed,
		},
		{
			name:   "failure after cancel is not a failure",
			plan:   mplan(false, mtask{name: "A"}, mtask{name: "B", deps: []string{"A"}}, mtask{name: "X"}),
			roots:  []string{"A", "X"},
			steps:  []mstep{cancelStep(false), fin("A", bad), fin("X", gone, "end:cancelled")},
			final:  WorkflowCancelled,
			states: map[string]JobState{"A": JobFailed, "B": JobCancelled, "X": JobCancelled},
		},
		{
			name:  "cancel while compensating: no effects",
			plan:  mplan(false, mtask{name: "A", undo: true}, mtask{name: "B", deps: []string{"A"}}),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B"), fin("B", bad, "A:undo"),
				cancelStep(true), cancelStep(false),
				finUndo("A", ok, "end:compensated"),
			},
			final: WorkflowCompensated,
		},
		{
			name:   "late finish of a cancelled task ignored",
			plan:   mplan(false, mtask{name: "A"}, mtask{name: "B", deps: []string{"A"}}, mtask{name: "X"}),
			roots:  []string{"A", "X"},
			steps:  []mstep{fin("A", bad), fin("B", ok), fin("X", ok, "end:failed")},
			final:  WorkflowFailed,
			states: map[string]JobState{"A": JobFailed, "B": JobCancelled, "X": JobCompleted},
		},
		{
			name:  "finish of a waiting task ignored",
			plan:  mplan(false, mtask{name: "A"}, mtask{name: "B", deps: []string{"A"}}),
			roots: []string{"A"},
			steps: []mstep{fin("B", ok), fin("A", ok, "B"), fin("B", ok, "end:completed")},
			final: WorkflowCompleted,
		},
		{
			name:  "idempotent finish",
			plan:  mplan(false, mtask{name: "A"}, mtask{name: "B", deps: []string{"A"}}, mtask{name: "X"}),
			roots: []string{"A", "X"},
			steps: []mstep{fin("A", ok, "B"), fin("A", ok), fin("A", bad), fin("B", ok), fin("B", ok), fin("X", ok, "end:completed"), fin("X", ok)},
			final: WorkflowCompleted,
		},
		{
			name: "idempotent undo finish",
			plan: mplan(false,
				mtask{name: "A", undo: true},
				mtask{name: "B", deps: []string{"A"}, undo: true},
				mtask{name: "C", deps: []string{"B"}},
			),
			roots: []string{"A"},
			steps: []mstep{
				fin("A", ok, "B"), fin("B", ok, "C"), fin("C", bad, "B:undo"),
				finUndo("A", ok), // still waiting: ignored
				finUndo("B", ok, "A:undo"), finUndo("B", ok),
				finUndo("A", ok, "end:compensated"), finUndo("A", ok),
			},
			final: WorkflowCompensated,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, roots := newWFState(tc.plan)
			assert.Equal(t, WorkflowRunning, s.State)
			assert.Equal(t, tc.roots, fxStrings(roots))
			for i, st := range tc.steps {
				var got []wfEffect
				if st.cancel {
					got = s.cancel(st.cundo)
				} else {
					var err error
					got, err = s.finish(TaskEnd{Workflow: "wf", Task: st.task, Outcome: st.out, Undo: st.undo})
					require.NoError(t, err, "step %d", i)
				}
				want := st.effects
				if want == nil {
					want = []string{}
				}
				assert.Equal(t, want, fxStrings(got), "step %d (%+v)", i, st)
			}
			assert.Equal(t, tc.final, s.State)
			for name, want := range tc.states {
				assert.Equal(t, want, s.Tasks[name].State, "task %s", name)
			}
			for name, want := range tc.undos {
				assert.Equal(t, want, s.Tasks[name].UndoState, "undo %s", name)
			}
		})
	}
}

func TestMachineNewState(t *testing.T) {
	s, _ := newWFState(mplan(false,
		mtask{name: "A"},
		mtask{name: "B", deps: []string{"A"}},
		mtask{name: "C", deps: []string{"A", "B"}},
	))
	assert.Equal(t, []string{"A", "B", "C"}, s.Order)
	assert.Equal(t, JobPending, s.Tasks["A"].State)
	assert.Equal(t, JobWaiting, s.Tasks["B"].State)
	assert.Equal(t, 1, s.Tasks["B"].Open)
	assert.Equal(t, 2, s.Tasks["C"].Open)
}

func TestMachineUnknownTask(t *testing.T) {
	s, _ := newWFState(mplan(false, mtask{name: "A"}))
	fx, err := s.finish(TaskEnd{Workflow: "wf", Task: "nope", Outcome: OutcomeCompleted})
	assert.Error(t, err)
	assert.Empty(t, fx)

	fx, err = s.finish(TaskEnd{Workflow: "wf", Task: "A", Outcome: "lost"})
	assert.Error(t, err)
	assert.Empty(t, fx)
	assert.Equal(t, JobPending, s.Tasks["A"].State)
}

func TestMachineKeepsReasonAndResult(t *testing.T) {
	s, _ := newWFState(mplan(false, mtask{name: "A"}, mtask{name: "B"}))
	_, err := s.finish(TaskEnd{Task: "A", Outcome: OutcomeCompleted, Result: []byte(`1`)})
	require.NoError(t, err)
	_, err = s.finish(TaskEnd{Task: "B", Outcome: OutcomeFailed, Reason: "boom"})
	require.NoError(t, err)
	assert.Equal(t, []byte(`1`), s.Tasks["A"].Result)
	assert.Equal(t, "boom", s.Tasks["B"].Reason)
}

func TestMachineProcessingFinishes(t *testing.T) {
	// storage marks a popped task processing; finish still applies.
	s, _ := newWFState(mplan(false, mtask{name: "A"}))
	s.Tasks["A"].State = JobProcessing
	fx, err := s.finish(TaskEnd{Task: "A", Outcome: OutcomeCompleted})
	require.NoError(t, err)
	assert.Equal(t, []string{"end:completed"}, fxStrings(fx))
}

func mustFinish(t *testing.T, s *wfState, e TaskEnd) []string {
	t.Helper()
	fx, err := s.finish(e)
	require.NoError(t, err)
	return fxStrings(fx)
}

func TestMachineRunnable(t *testing.T) {
	s, _ := newWFState(mplan(false,
		mtask{name: "A", undo: true},
		mtask{name: "B", deps: []string{"A"}},
		mtask{name: "C", deps: []string{"B"}},
		mtask{name: "X"},
	))
	assert.True(t, s.runnable("A", false))
	assert.False(t, s.runnable("A", true), "no undo while running")
	assert.False(t, s.runnable("B", false), "waiting task")
	assert.False(t, s.runnable("nope", false))

	assert.Equal(t, []string{"B"}, mustFinish(t, s, TaskEnd{Task: "A", Outcome: OutcomeCompleted}))
	assert.False(t, s.runnable("A", false), "completed task")
	assert.True(t, s.runnable("B", false))

	s.Tasks["X"].State = JobProcessing
	assert.False(t, s.runnable("X", false), "processing task")
	s.Tasks["X"].State = JobPending

	assert.Empty(t, s.cancel(true))
	assert.False(t, s.runnable("B", false), "pending task of a cancelled workflow")
	assert.False(t, s.runnable("C", false), "cancelled task")

	assert.Empty(t, mustFinish(t, s, TaskEnd{Task: "B", Outcome: OutcomeCancelled}))
	assert.False(t, s.runnable("X", false))
	assert.Equal(t, []string{"A:undo"}, mustFinish(t, s, TaskEnd{Task: "X", Outcome: OutcomeCancelled}))
	assert.Equal(t, WorkflowCompensating, s.State)
	assert.True(t, s.runnable("A", true), "pending undo while compensating")
	assert.False(t, s.runnable("A", false), "forward task while compensating")
	assert.False(t, s.runnable("B", true), "task with no undo")

	assert.Equal(t, []string{"end:compensated"}, mustFinish(t, s, TaskEnd{Task: "A", Outcome: OutcomeCompleted, Undo: true}))
	assert.False(t, s.runnable("A", true), "ended undo, ended workflow")
}

func TestMachineDeterministic(t *testing.T) {
	// same input, same effects, run after run (no map order).
	plan := mplan(false,
		mtask{name: "r", undo: true},
		mtask{name: "e", deps: []string{"r"}, undo: true},
		mtask{name: "a", deps: []string{"r"}, undo: true},
		mtask{name: "q", deps: []string{"r"}, undo: true},
		mtask{name: "z", deps: []string{"e", "a", "q"}},
	)
	var first []string
	for range 50 {
		s, roots := newWFState(plan)
		all := fxStrings(roots)
		for _, n := range []string{"r", "q", "a", "e"} {
			all = append(all, mustFinish(t, s, TaskEnd{Task: n, Outcome: OutcomeCompleted})...)
		}
		all = append(all, mustFinish(t, s, TaskEnd{Task: "z", Outcome: OutcomeFailed})...)
		for _, n := range []string{"q", "a", "e", "r"} {
			all = append(all, mustFinish(t, s, TaskEnd{Task: n, Outcome: OutcomeCompleted, Undo: true})...)
		}
		if first == nil {
			first = all
			assert.Equal(t, []string{
				"r", "e", "a", "q", "z", "e:undo", "a:undo", "q:undo",
				"r:undo", "end:compensated",
			}, all)
			continue
		}
		require.True(t, slices.Equal(first, all), "run differs: %v vs %v", first, all)
	}
}

// TestMachineFailFastRunnable: after FailFast fires, a queued forward task
// may not start; Always tasks (the failed task's kept dependents) still run.
func TestMachineFailFastRunnable(t *testing.T) {
	s, _ := newWFState(mplan(true,
		mtask{name: "A"},
		mtask{name: "D", deps: []string{"A"}, always: true},
		mtask{name: "E", deps: []string{"A"}}, // so A's failure is unabsorbed
		mtask{name: "X"},
		mtask{name: "Y", always: true},
	))
	assert.True(t, s.runnable("X", false))

	assert.Equal(t, []string{"D"}, mustFinish(t, s, TaskEnd{Task: "A", Outcome: OutcomeFailed}))
	assert.False(t, s.runnable("X", false), "queued, not started: FailFast stops it")
	assert.True(t, s.runnable("D", false), "kept Always dependent runs")
	assert.True(t, s.runnable("Y", false), "Always runs whatever failed")

	s, _ = newWFState(mplan(false, mtask{name: "A"}, mtask{name: "X"}))
	mustFinish(t, s, TaskEnd{Task: "A", Outcome: OutcomeFailed})
	assert.True(t, s.runnable("X", false), "no FailFast: unrelated branches finish")
}

// TestMachineUndoOnCancel: the plan's UndoOnCancel holds until a cancel
// gives its own.
func TestMachineUndoOnCancel(t *testing.T) {
	for _, tc := range []struct {
		plan, cancel bool
		want         []string
	}{
		{true, true, []string{"A:undo"}},
		{true, false, []string{"end:cancelled"}},
		{false, true, []string{"A:undo"}},
		{false, false, []string{"end:cancelled"}},
	} {
		p := mplan(false, mtask{name: "A", undo: true}, mtask{name: "B", deps: []string{"A"}})
		p.UndoOnCancel = tc.plan
		s, _ := newWFState(p)
		assert.Equal(t, tc.plan, s.UndoOnCancel)
		mustFinish(t, s, TaskEnd{Task: "A", Outcome: OutcomeCompleted})
		assert.Empty(t, s.cancel(tc.cancel), "B is queued")
		assert.Equal(t, tc.cancel, s.UndoOnCancel, "the cancel's value is kept")
		assert.Equal(t, tc.want, mustFinish(t, s, TaskEnd{Task: "B", Outcome: OutcomeCancelled}), "%+v", tc)
	}
}

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
	require.NoError(t, s.finish(e))
	assert.Equal(t, 1, prio.Len())

	e = end("rec", "b", OutcomeFailed)
	e.Reason = "boom"
	require.NoError(t, s.finish(e))
	assert.Equal(t, 2, fifo.Len(), "c is Always: runs anyway")

	r, _ = s.Record(ctx, "rec")
	assert.Equal(t, JobCompleted, r.Tasks[0].State)
	assert.Equal(t, 7, r.Tasks[0].ResultSize)
	assert.Equal(t, JobFailed, r.Tasks[1].State)
	assert.Equal(t, "boom", r.Tasks[1].Reason)
	assert.Equal(t, JobPending, r.Tasks[2].State)
	assert.Zero(t, r.FinishedAt)

	*clock = clock.Add(time.Second)
	err = s.finish(end("rec", "c", OutcomeCompleted))
	require.NoError(t, err)
	r, _ = s.Record(ctx, "rec")
	assert.Equal(t, WorkflowCompleted, r.State, "b's failure is absorbed by Always c")
	assert.Equal(t, clock.UnixMilli(), r.FinishedAt)

	_, err = s.Record(ctx, "nope")
	assert.ErrorIs(t, err, ErrWorkflowNotFound)
	err = s.finish(end("nope", "a", OutcomeCompleted))
	assert.ErrorIs(t, err, ErrWorkflowNotFound)
	err = s.finish(end("rec", "zzz", OutcomeCompleted))
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
	err := s.finish(end("old", "a", OutcomeCompleted))
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
	s, clock := newMemStore(t)

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()
	for _, id := range []string{"w1", "w2", "w3", "w4"} {
		wf := NewWorkflow(s, WithWorkflowID(id))
		wf.Add("a", q, 1)
		require.NoError(t, submit(t, wf))
		*clock = clock.Add(time.Millisecond)
	}
	err := s.finish(end("w2", "a", OutcomeCompleted))
	require.NoError(t, err)
	err = s.finish(end("w3", "a", OutcomeFailed))
	require.NoError(t, err)

	list, _ := s.Workflows(ctx, "", 0, 10)
	assert.Equal(t, []string{"w4", "w3", "w2", "w1"}, ids(list), "newest first")
	list, _ = s.Workflows(ctx, "", 1, 2)
	assert.Equal(t, []string{"w3", "w2"}, ids(list))
	list, _ = s.Workflows(ctx, WorkflowRunning, 0, 10)
	assert.Equal(t, []string{"w4", "w1"}, ids(list))
	list, _ = s.Workflows(ctx, WorkflowFailed, 0, 10)
	assert.Equal(t, []string{"w3"}, ids(list))
	list, err = s.Workflows(ctx, "", math.MaxInt, math.MaxInt)
	require.NoError(t, err)
	assert.Empty(t, list)
	assert.NotNil(t, list, "an empty page is [], not null")
	list, err = s.Workflows(ctx, "", 0, 0)
	require.NoError(t, err)
	assert.Empty(t, list, "limit 0")
}

// TestMemStoreWorkflowsTies: workflows of one ms list by ID, as GET
// /workflows merges, so pages tile with no overlap or drop.
func TestMemStoreWorkflowsTies(t *testing.T) {
	ctx := context.Background()
	s, clock := newMemStore(t)

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()
	for _, id := range []string{"old", "c", "a", "d", "b"} {
		wf := NewWorkflow(s, WithWorkflowID(id))
		wf.Add("a", q, 1)
		require.NoError(t, submit(t, wf))
		if id == "old" {
			*clock = clock.Add(time.Millisecond) // the rest share one ms
		}
	}

	list, _ := s.Workflows(ctx, "", 0, 10)
	assert.Equal(t, []string{"a", "b", "c", "d", "old"}, ids(list))
	var paged []string
	for off := range 5 {
		keys, err := s.WorkflowKeys(ctx, "", off, 1)
		require.NoError(t, err)
		require.Len(t, keys, 1)
		paged = append(paged, keys[0].ID)
	}
	assert.Equal(t, ids(list), paged, "limit 1 pages tile")
}

// TestMemStoreClockBack: a wall clock stepped back between Submits does not
// date a workflow before an earlier one, so the newest still lists first.
func TestMemStoreClockBack(t *testing.T) {
	ctx := context.Background()
	s, clock := newMemStore(t)

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()
	for _, step := range []struct {
		id string
		ms int64
	}{{"a", 100}, {"b", 200}, {"c", 150}} {
		*clock = time.UnixMilli(step.ms)
		wf := NewWorkflow(s, WithWorkflowID(step.id))
		wf.Add("a", q, 1)
		require.NoError(t, submit(t, wf))
	}

	list, err := s.Workflows(ctx, "", 0, 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, ids(list), "b and c tie at 200: by ID")
	r, err := s.Record(ctx, "c")
	require.NoError(t, err)
	assert.Equal(t, int64(200), r.CreatedAt, "clamped to b's")
	list, _ = s.Workflows(ctx, "", 0, 10)
	assert.Equal(t, []string{"b", "c", "a"}, ids(list))
}

// TestMemStoreListBounded: a state-filtered listing scans at most
// maxWorkflowsOffset+limit workflows, matched or not; keys list the same.
func TestMemStoreListBounded(t *testing.T) {
	ctx := context.Background()
	s, _ := newMemStore(t)

	// Built directly: 10k Submits are slow under -race.
	n := maxWorkflowsOffset + 2
	for i := range n {
		st := WorkflowRunning
		if i == 0 {
			st = WorkflowCompleted // the oldest, past the bound
		}
		s.order = append(s.order, &memWorkflow{id: fmt.Sprint("b", i), created: int64(i), s: &wfState{State: st}})
	}

	list, err := s.Workflows(ctx, WorkflowCompleted, 0, 1)
	require.NoError(t, err)
	assert.Empty(t, list, "scan stops at maxWorkflowsOffset+limit")
	list, _ = s.Workflows(ctx, WorkflowCompleted, 0, 2)
	assert.Equal(t, []string{"b0"}, ids(list), "a bigger limit reaches it")
	list, _ = s.Workflows(ctx, "", n-1, 1)
	assert.Equal(t, []string{"b0"}, ids(list), "no filter: no bound")

	keys, err := s.WorkflowKeys(ctx, WorkflowRunning, 1, 2)
	require.NoError(t, err)
	want := []WorkflowKey{{ID: fmt.Sprint("b", n-2), CreatedAt: int64(n - 2)}, {ID: fmt.Sprint("b", n-3), CreatedAt: int64(n - 3)}}
	assert.Equal(t, want, keys)
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
	err = s.finish(e)
	require.NoError(t, err)
	e.Result[0] = 'X' // the store keeps its own copy
	got, err := s.DepResult(ctx, "res", "a")
	require.NoError(t, err)
	assert.Equal(t, []byte("1234"), got)
	got[0] = 'Y'
	got, _ = s.DepResult(ctx, "res", "a")
	assert.Equal(t, []byte("1234"), got, "callers get a copy")

	err = s.finish(end("res", "b", OutcomeFailed))
	require.NoError(t, err)
	_, err = s.DepResult(ctx, "res", "b")
	assert.ErrorIs(t, err, ErrDepFailed)

	e = end("res", "big", OutcomeCompleted)
	e.Result = []byte("12345")
	err = s.finish(e)
	require.NoError(t, err)
	_, err = s.DepResult(ctx, "res", "big")
	assert.ErrorIs(t, err, ErrDepFailed, "over the limit: failed")
	r, _ := s.Record(ctx, "res")
	assert.Equal(t, JobFailed, r.Tasks[2].State)
	assert.Equal(t, ErrResultTooLarge.Error(), r.Tasks[2].Reason)
	assert.Zero(t, r.Tasks[2].ResultSize)

	err = s.finish(end("res", "none", OutcomeCompleted))
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
	err := s.finish(end("cx", "a", OutcomeCompleted))
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

	err = s.finish(end("cx", "b", OutcomeCancelled))
	require.NoError(t, err)
	r, _ = s.Record(ctx, "cx")
	assert.Equal(t, WorkflowCompensating, r.State)
	assert.Equal(t, JobPending, r.Tasks[0].Undo.State)
	require.Equal(t, 1, q.Len(), "a's undo pushed")
	v, _ := q.(*queue[int]).internalQueue.Dequeue()
	assert.Equal(t, "wf:cx:a:undo", v.(*job[int]).ID())

	ok, _ = s.Runnable(ctx, "cx", "a", true)
	assert.True(t, ok)
	err = s.finish(TaskEnd{Workflow: "cx", Task: "a", Outcome: OutcomeCompleted, Undo: true})
	require.NoError(t, err)
	r, _ = s.Record(ctx, "cx")
	assert.Equal(t, WorkflowCompensated, r.State)
	assert.NotZero(t, r.FinishedAt)

	require.NoError(t, s.Cancel(ctx, "cx", nil), "cancel of an ended workflow: no-op")
	assert.ErrorIs(t, s.Cancel(ctx, "nope", nil), ErrWorkflowNotFound)
	_, err = s.Runnable(ctx, "nope", "a", false)
	assert.ErrorIs(t, err, ErrWorkflowNotFound)
}

// TestMemStoreUndoReason: a failed undo keeps its reason in the record.
func TestMemStoreUndoReason(t *testing.T) {
	ctx := context.Background()
	s, _ := newMemStore(t)

	w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
	q := w.BindQueue()
	wf := NewWorkflow(s, WithWorkflowID("ur"))
	a := wf.Add("a", q, 1).Undo(q, 10)
	wf.Add("b", q, 2).After(a)
	require.NoError(t, submit(t, wf))
	err := s.finish(end("ur", "a", OutcomeCompleted))
	require.NoError(t, err)
	err = s.finish(end("ur", "b", OutcomeFailed))
	require.NoError(t, err)

	r, _ := s.Record(ctx, "ur")
	require.Equal(t, WorkflowCompensating, r.State)
	assert.Empty(t, r.Tasks[0].Undo.Reason)

	err = s.finish(TaskEnd{Workflow: "ur", Task: "a", Outcome: OutcomeFailed, Reason: "undo boom", Undo: true})
	require.NoError(t, err)
	r, _ = s.Record(ctx, "ur")
	assert.Equal(t, WorkflowCompensationFailed, r.State)
	assert.Equal(t, JobFailed, r.Tasks[0].Undo.State)
	assert.Equal(t, "undo boom", r.Tasks[0].Undo.Reason)
	assert.Empty(t, r.Tasks[0].Reason, "the task's own reason is apart")
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

		require.NoError(t, s.finish(end("cap", "a", OutcomeCompleted)))
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
		require.NoError(t, s.finish(end("ref", "a", OutcomeCompleted)))

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
			err := s.finish(end(id, "a", OutcomeCompleted))
			assert.NoError(t, err)
			_, _ = s.Workflows(ctx, "", 0, 5)
			err = s.finish(end(id, "b", OutcomeCompleted))
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

// waitWF waits for wf to end, failing the test after a few seconds.
func waitWF(t *testing.T, wf *Workflow) WorkflowRecord {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := wf.Wait(ctx)
	require.NoError(t, err)

	return r
}

// runLog records what ran, in start order.
type runLog struct {
	mu  sync.Mutex
	ran []string
}

func (l *runLog) add(s string) {
	l.mu.Lock()
	l.ran = append(l.ran, s)
	l.mu.Unlock()
}

func (l *runLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.ran...)
}

func TestWorkflowRunsDAG(t *testing.T) {
	ctx := context.Background()
	store := NewWorkflowStore()
	var log runLog

	upQ := NewResultWorker(func(j Job[string]) (string, error) {
		log.add("upload")
		return "file:" + j.Data(), nil
	}).BindQueue()
	thumbQ := NewResultWorker(func(j Job[int]) (int, error) {
		log.add(fmt.Sprint("thumb-", j.Data()))
		return j.Data() * 10, nil
	}, 2).BindQueue()

	var got []int
	mailQ := NewErrWorker(func(j Job[string]) error {
		log.add("email")
		for _, dep := range []string{"thumb-s", "thumb-l"} {
			var n int
			if err := TaskOf(j).Result(dep, &n); err != nil {
				return err
			}
			got = append(got, n)
		}
		return nil
	}).BindQueue()

	wf := NewWorkflow(store, WithWorkflowID("dag"))
	up := wf.Add("upload", upQ, "x")
	s := wf.Add("thumb-s", thumbQ, 64).After(up)
	l := wf.Add("thumb-l", thumbQ, 512).After(up)
	wf.Add("email", mailQ, "to").After(s, l)
	require.NoError(t, wf.Submit(ctx))

	r := waitWF(t, wf)
	assert.Equal(t, WorkflowCompleted, r.State)
	ran := log.list()
	require.Len(t, ran, 4)
	assert.Equal(t, "upload", ran[0])
	assert.ElementsMatch(t, []string{"thumb-64", "thumb-512"}, ran[1:3])
	assert.Equal(t, "email", ran[3], "email after both thumbs")
	assert.Equal(t, []int{640, 5120}, got)

	b, err := store.DepResult(ctx, "dag", "upload")
	require.NoError(t, err)
	assert.JSONEq(t, `"file:x"`, string(b))
	assert.Equal(t, len(b), r.Tasks[0].ResultSize)
	assert.Zero(t, r.Tasks[3].ResultSize, "an error worker leaves no result")
}

func TestWorkflowDiamondAlways(t *testing.T) {
	ctx := context.Background()
	store := NewWorkflowStore()
	var dRuns atomic.Int32
	var bErr, cErr error

	q := NewResultWorker(func(j Job[string]) (string, error) {
		switch j.Data() {
		case "B":
			return "", errors.New("b broke")
		case "D":
			dRuns.Add(1)
			_, bErr = store.DepResult(ctx, "diamond", "B")
			_, cErr = store.DepResult(ctx, "diamond", "C")
		}
		return j.Data(), nil
	}, 2).BindQueue()

	wf := NewWorkflow(store, WithWorkflowID("diamond"))
	a := wf.Add("A", q, "A")
	b := wf.Add("B", q, "B").After(a)
	c := wf.Add("C", q, "C").After(a)
	wf.Add("D", q, "D").After(b, c).Always()
	require.NoError(t, wf.Submit(ctx))

	r := waitWF(t, wf)
	assert.Equal(t, WorkflowCompleted, r.State, "B's failure is absorbed by Always D")
	assert.Equal(t, int32(1), dRuns.Load())
	assert.ErrorIs(t, bErr, ErrDepFailed)
	assert.NoError(t, cErr)
	assert.Equal(t, JobFailed, r.Tasks[1].State)
	assert.Equal(t, "b broke", r.Tasks[1].Reason)
	assert.Equal(t, JobCompleted, r.Tasks[3].State)
}

func TestWorkflowResultTooLarge(t *testing.T) {
	ctx := context.Background()
	var ran atomic.Int32
	q := NewResultWorker(func(j Job[int]) (string, error) {
		ran.Add(1)
		return strings.Repeat("x", 65<<10), nil
	}).BindQueue()

	wf := NewWorkflow(NewWorkflowStore(), WithWorkflowID("big"))
	a := wf.Add("a", q, 1)
	wf.Add("b", q, 2).After(a)
	require.NoError(t, wf.Submit(ctx))

	r := waitWF(t, wf)
	assert.Equal(t, WorkflowFailed, r.State)
	assert.Equal(t, JobFailed, r.Tasks[0].State)
	assert.Equal(t, ErrResultTooLarge.Error(), r.Tasks[0].Reason)
	assert.Equal(t, JobCancelled, r.Tasks[1].State)
	assert.Equal(t, int32(1), ran.Load(), "b never ran")

	m := q.(*resultQueue[int, string]).totals
	assert.Equal(t, uint64(1), m.failed.Load(), "the queue counts it failed")
	assert.Zero(t, m.completed.Load())
}

func TestWorkflowCancelSkipsQueued(t *testing.T) {
	ctx := context.Background()
	var ran atomic.Int32
	w := NewWorker(func(Job[int]) { ran.Add(1) })
	q := w.BindQueue()
	require.NoError(t, w.PauseAndWait())

	wf := NewWorkflow(NewWorkflowStore(), WithWorkflowID("cq"))
	a := wf.Add("a", q, 1)
	wf.Add("b", q, 2).After(a)
	require.NoError(t, wf.Submit(ctx))
	require.NoError(t, wf.Cancel(ctx))
	require.NoError(t, w.Resume())

	r := waitWF(t, wf)
	assert.Equal(t, WorkflowCancelled, r.State)
	assert.Equal(t, JobCancelled, r.Tasks[0].State)
	assert.Equal(t, "workflow cancelled", r.Tasks[0].Reason)
	assert.Equal(t, JobCancelled, r.Tasks[1].State)
	w.Wait()
	assert.Zero(t, ran.Load(), "the queued task never ran")
	assert.Zero(t, q.Len())
}

// TestWorkflowUndoOnCancelFrozen: WithUndoOnCancel is kept with the workflow,
// so a cancel from any handle (or REST without undo) follows it; the store's
// undo overrides it.
func TestWorkflowUndoOnCancelFrozen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		id     string
		cancel func(t *testing.T, store WorkflowStore, id string)
		want   WorkflowState
		undos  int32
	}{
		{"OpenWorkflow", "frozen-open", func(t *testing.T, store WorkflowStore, id string) {
			require.NoError(t, OpenWorkflow(store, id).Cancel(context.Background()))
		}, WorkflowCancelled, 0},
		{"REST without undo", "frozen-rest", func(t *testing.T, _ WorkflowStore, id string) {
			rr := httptest.NewRecorder()
			NewServerMux().ServeHTTP(rr, httptest.NewRequest("PATCH", "/workflows/"+id+"/actions/cancel", nil))
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		}, WorkflowCancelled, 0},
		{"store undo=true overrides", "frozen-store-undo", func(t *testing.T, store WorkflowStore, id string) {
			require.NoError(t, store.Cancel(context.Background(), id, new(true))) // REST refuses true
		}, WorkflowCompensated, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			started, release := make(chan struct{}), make(chan struct{})
			var undos atomic.Int32
			q := NewWorker(func(j Job[int]) {
				switch j.Data() {
				case 2:
					close(started)
					<-release
				case 10:
					undos.Add(1)
				}
			}, 2).BindQueue()

			store := NewWorkflowStore()
			wf := NewWorkflow(store, WithWorkflowID(tc.id), WithUndoOnCancel(false))
			a := wf.Add("a", q, 1).Undo(q, 10)
			wf.Add("b", q, 2).After(a)
			require.NoError(t, wf.Submit(ctx))

			<-started // a completed, b runs
			tc.cancel(t, store, tc.id)
			close(release)

			r := waitWF(t, wf)
			assert.Equal(t, tc.want, r.State)
			assert.Equal(t, tc.undos, undos.Load())
			assert.Equal(t, tc.undos == 1, r.UndoOnCancel, "the record shows the cancel's choice")
		})
	}
}

func TestWorkflowPurgeCancels(t *testing.T) {
	ctx := context.Background()
	var ran atomic.Int32
	w := NewWorker(func(Job[int]) { ran.Add(1) })
	q := w.BindQueue()
	require.NoError(t, w.PauseAndWait())

	wf := NewWorkflow(NewWorkflowStore(), WithWorkflowID("purge"))
	a := wf.Add("a", q, 1)
	wf.Add("b", q, 2).After(a)
	require.NoError(t, wf.Submit(ctx))
	q.Purge()

	r := waitWF(t, wf)
	assert.Equal(t, WorkflowFailed, r.State)
	assert.Equal(t, JobCancelled, r.Tasks[0].State, "purged: cancelled, never completed")
	assert.Equal(t, JobCancelled, r.Tasks[1].State)
	require.NoError(t, w.Resume())
	w.Wait()
	assert.Zero(t, ran.Load())
}

func TestWorkflowUndoRuns(t *testing.T) {
	ctx := context.Background()
	var log runLog
	q := NewErrWorker(func(j Job[string]) error {
		log.add(j.Data())
		if j.Data() == "b" {
			return errors.New("no")
		}
		return nil
	}).BindQueue()
	undoQ := NewResultWorker(func(j Job[string]) (string, error) {
		log.add(j.Data())
		return "ignored", nil
	}).BindQueue()

	wf := NewWorkflow(NewWorkflowStore(), WithWorkflowID("saga"))
	a := wf.Add("a", q, "a").Undo(undoQ, "undo-a")
	wf.Add("b", q, "b").After(a)
	require.NoError(t, wf.Submit(ctx))

	r := waitWF(t, wf)
	assert.Equal(t, WorkflowCompensated, r.State)
	assert.Equal(t, []string{"a", "b", "undo-a"}, log.list())
	assert.Equal(t, JobCompleted, r.Tasks[0].Undo.State)
	assert.Zero(t, r.Tasks[0].ResultSize, "an undo's result is not kept")
}

// crashState is the workflow state behind crashStorage: a store's
// FinishTask, in a map, not Redis.
type crashState struct {
	mu      sync.Mutex
	s       *wfState
	finishs int
	lose    func(call int) (applied, lost bool) // what the call does
	pushedB int
	bQueue  *mocks.MockPersistentQueue
	bItem   []byte
	runErr  error     // Runnable's error, if set
	ends    []TaskEnd // every FinishTask's end
}

// crashStorage is a durable, workflow-acknowledging mock queue.
type crashStorage struct {
	*mocks.MockPersistentQueue
	wfStorage
	st *crashState
}

type crashStore struct {
	fakeWFStore
	st *crashState
}

func (c crashStore) Runnable(_ context.Context, _, task string, undo bool) (bool, error) {
	c.st.mu.Lock()
	defer c.st.mu.Unlock()
	if c.st.runErr != nil {
		return false, c.st.runErr
	}
	return c.st.s.runnable(task, undo), nil
}

func (c crashStorage) WorkflowStore() WorkflowStore { return crashStore{memStore(), c.st} }

func (c crashStorage) FinishTask(_ string, e TaskEnd) ([]string, bool) {
	st := c.st
	st.mu.Lock()
	defer st.mu.Unlock()

	st.finishs++
	st.ends = append(st.ends, e)
	applied, lost := st.lose(st.finishs)
	if lost { // the reply never reaches the caller, whatever was applied
		if applied {
			st.finish(e)
		}
		return nil, false
	}
	if t := st.s.Tasks[e.Task]; t != nil && !e.Undo && (t.State == JobCompleted || t.State == JobFailed || t.State == JobCancelled) {
		// Already ended: nothing changes; the contract returns every
		// push-target queue, so a lost wake is made up.
		var all []string
		for _, name := range st.s.Order {
			all = append(all, "q-"+name)
		}
		return all, true
	}

	return st.finish(e), true
}

// finish applies e and pushes b when released. Callers hold mu.
func (st *crashState) finish(e TaskEnd) []string {
	var pushed []string
	fx, _ := st.s.finish(e) // plan tasks and known outcomes only
	for _, f := range fx {
		if f.Push == "b" {
			st.pushedB++
			st.bQueue.Queue.Enqueue(st.bItem) // no wake: FinishTask's caller pokes
			pushed = append(pushed, "q-b")
		}
	}

	return pushed
}

func taskBytes(t *testing.T, wf, task string, data int) []byte {
	t.Helper()
	j := newJob(data, jobConfigs{Id: "wf:" + wf + ":" + task})
	j.setTask(taskTag{wf: wf, task: task})
	b, err := j.Json()
	require.NoError(t, err)

	return b
}

// TestWorkflowCrashRerun: a's FinishTask is lost and a is delivered again;
// b is pushed once either way.
func TestWorkflowCrashRerun(t *testing.T) {
	for _, tc := range []struct {
		name    string
		applied bool // the lost call recorded the end (reply lost) or not (crash before)
		aRuns   int32
		finishs int // b's own finish included once it runs
	}{
		{"reply lost", true, 1, 3},
		{"crash before finish", false, 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := WorkflowPlan{ID: "cr", Tasks: []TaskPlan{{Name: "a"}, {Name: "b", Deps: []string{"a"}}}}
			ws, _ := newWFState(plan)
			st := &crashState{s: ws, bQueue: mocks.NewMockPersistentQueue(),
				bItem: taskBytes(t, "cr", "b", 2)}
			st.lose = func(call int) (bool, bool) {
				if call == 1 {
					return tc.applied, true
				}
				return true, false
			}
			aStorage := crashStorage{mocks.NewMockPersistentQueue(), wfStorage{"q-a", "s"}, st}
			bStorage := crashStorage{st.bQueue, wfStorage{"q-b", "s"}, st}

			var aRuns, bRuns atomic.Int32
			aw := NewWorker(func(Job[int]) { aRuns.Add(1) })
			aw.WithPersistentQueue(aStorage)
			bw := NewWorker(func(Job[int]) { bRuns.Add(1) })
			bw.WithPersistentQueue(bStorage)

			aItem := taskBytes(t, "cr", "a", 1)
			aStorage.Queue.Enqueue(aItem)
			aw.notifyToPullNextJobs()
			select {
			case err := <-aw.Errs():
				assert.ErrorIs(t, err, ErrAcknowledgeJob)
			case <-time.After(5 * time.Second):
				t.Fatal("no ack error for the lost FinishTask")
			}

			aStorage.Queue.Enqueue(aItem) // redelivered, as a visibility timeout would
			aw.notifyToPullNextJobs()
			aw.Wait()
			// Either way b's local consumer is poked: by the reply that pushed
			// b, or by the re-finish of ended a.
			require.Eventually(t, func() bool { return bRuns.Load() == 1 }, 5*time.Second, time.Millisecond)
			bw.Wait()

			st.mu.Lock()
			defer st.mu.Unlock()
			assert.Equal(t, 1, st.pushedB, "b pushed once")
			assert.Equal(t, tc.finishs, st.finishs)
			assert.Equal(t, tc.aRuns, aRuns.Load())
			assert.Equal(t, JobCompleted, st.s.Tasks["a"].State)
			assert.Equal(t, int32(1), bRuns.Load(), "b runs once")
		})
	}
}

// TestWorkflowPreRunError: a workflow the store no longer holds cancels the
// task unrun; any other Runnable error is reported and the task runs.
func TestWorkflowPreRunError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		runs int32
		end  Outcome
	}{
		{"not found", fmt.Errorf("%w: cr", ErrWorkflowNotFound), 0, OutcomeCancelled},
		{"other error", errors.New("store down"), 1, OutcomeCompleted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := WorkflowPlan{ID: "cr", Tasks: []TaskPlan{{Name: "a"}}}
			ws, _ := newWFState(plan)
			st := &crashState{s: ws, runErr: tc.err, lose: func(int) (bool, bool) { return true, false }}
			storage := crashStorage{mocks.NewMockPersistentQueue(), wfStorage{"q-a", "s"}, st}

			var runs atomic.Int32
			w := NewWorker(func(Job[int]) { runs.Add(1) })
			w.WithPersistentQueue(storage)
			storage.Queue.Enqueue(taskBytes(t, "cr", "a", 1))
			w.notifyToPullNextJobs()
			require.Eventually(t, func() bool {
				st.mu.Lock()
				defer st.mu.Unlock()
				return len(st.ends) == 1
			}, 5*time.Second, time.Millisecond)

			assert.Equal(t, tc.runs, runs.Load())
			st.mu.Lock()
			defer st.mu.Unlock()
			assert.Equal(t, tc.end, st.ends[0].Outcome)
			if tc.runs == 0 {
				assert.Equal(t, "workflow cancelled", st.ends[0].Reason)
				return
			}
			select {
			case err := <-w.Errs():
				assert.ErrorIs(t, err, tc.err)
			default:
				t.Fatal("the Runnable error was not reported")
			}
		})
	}
}

// ackSignal is a mock persistent queue that signals each settle.
type ackSignal struct {
	*mocks.MockPersistentQueue
	done chan struct{}
}

func (a ackSignal) Acknowledge(id string) bool {
	ok := a.MockPersistentQueue.Acknowledge(id)
	a.done <- struct{}{}
	return ok
}

func TestNonWorkflowJobUnchanged(t *testing.T) {
	t.Run("plain durable jobs acknowledge and reject", func(t *testing.T) {
		pq := mocks.NewMockPersistentQueue()
		w := NewErrWorker(func(j Job[int]) error {
			if j.Data() == 2 {
				return errors.New("bad")
			}
			return nil
		})
		p := w.WithQueue(pq)
		_, ok1 := p.Add(1)
		_, ok2 := p.Add(2)
		require.True(t, ok1 && ok2)
		require.Eventually(t, func() bool { return len(pq.Acked())+len(pq.Rejected()) == 2 },
			5*time.Second, time.Millisecond)
		assert.Equal(t, []string{"mock-ack-id"}, pq.Acked())
		assert.Equal(t, []string{"bad"}, pq.Rejected())
	})

	// Baselines measured on ab4614a (before workflow finish) with this harness.
	t.Run("allocs", func(t *testing.T) {
		if raceEnabled {
			t.Skip("the race detector allocates")
		}
		q := NewWorker(func(Job[int]) {}).BindQueue()
		mem := testing.AllocsPerRun(1000, func() {
			j, _ := q.Add(1)
			j.Wait()
		})

		pq := ackSignal{mocks.NewMockPersistentQueue(), make(chan struct{}, 1)}
		p := NewWorker(func(Job[int]) {}).WithPersistentQueue(pq)
		durable := testing.AllocsPerRun(1000, func() {
			p.Add(1)
			<-pq.done
		})

		t.Logf("allocs: in-memory %v, durable %v", mem, durable)
		assert.LessOrEqual(t, mem, float64(1))
		assert.LessOrEqual(t, durable, float64(8))
	})
}

func TestTaskOf(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, TaskOf(newJob("plain", jobConfigs{Id: "plain"})), "plain job: no task")

	var (
		upErr, strangerErr, failedErr error
		upVal                         string
		failed, okDep, stranger       bool
		name, wfID                    string
		undo, undoCtx                 bool
		undoOwn                       string
		undoOwnErr                    error
	)
	q := NewResultWorker(func(j Job[string]) (string, error) {
		switch j.Data() {
		case "up":
			return "file", nil
		case "bad", "boom":
			return "", errors.New(j.Data())
		case "join":
			tc := TaskOf(j)
			name, wfID, undo = tc.Name, tc.WorkflowID, tc.Undo
			upErr = tc.Result("up", &upVal)
			strangerErr = tc.Result("stranger", &upVal)
			failedErr = tc.Result("bad", &upVal)
			failed, okDep, stranger = tc.Failed("bad"), tc.Failed("up"), tc.Failed("stranger")
		case "undo-up":
			tc := TaskOf(j)
			undoOwnErr = tc.Result("up", &undoOwn)
			undoCtx = tc.Undo
		}
		return j.Data(), nil
	}, 2).BindQueue()

	wf := NewWorkflow(NewWorkflowStore(), WithWorkflowID("tof"))
	up := wf.Add("up", q, "up").Undo(q, "undo-up")
	bad := wf.Add("bad", q, "bad").After(up)
	join := wf.Add("join", q, "join").After(up, bad).Always()
	wf.Add("boom", q, "boom").After(join)
	require.NoError(t, wf.Submit(ctx))

	r := waitWF(t, wf)
	assert.Equal(t, WorkflowCompensated, r.State, "boom fails the workflow; up is undone")

	assert.Equal(t, "join", name)
	assert.Equal(t, "tof", wfID)
	assert.NoError(t, upErr)
	assert.Equal(t, "file", upVal)
	assert.ErrorIs(t, strangerErr, ErrNoResult, "stranger is no dep")
	assert.ErrorIs(t, failedErr, ErrDepFailed)
	assert.True(t, failed)
	assert.False(t, okDep)
	assert.False(t, stranger, "a non-dep never reads as failed")

	assert.NoError(t, undoOwnErr, "an undo reads its own task's result")
	assert.Equal(t, "file", undoOwn)
	assert.True(t, undoCtx)
	assert.False(t, undo)
}

// endRec records every TaskEnd FinishTask gets, by task.
type endRec struct {
	mu   sync.Mutex
	ends map[string]TaskEnd
}

func (r *endRec) finish(e TaskEnd) ([]string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends[e.Task] = e
	return nil, true
}

func (r *endRec) get() map[string]TaskEnd {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.ends)
}

// endStorage is durable storage popping one job per call; endBatchStorage
// pops batches (IBatchAcknowledgeable). Both record task ends.
type endStorage struct {
	*mocks.MockPersistentQueue
	wfStorage
	rec *endRec
}

func (s endStorage) FinishTask(_ string, e TaskEnd) ([]string, bool) { return s.rec.finish(e) }

type endBatchStorage struct {
	*batchTestQueue
	wfStorage
	rec *endRec
}

func (s endBatchStorage) FinishTask(_ string, e TaskEnd) ([]string, bool) { return s.rec.finish(e) }

// TestWorkflowDurableEnds: result and error workers on durable storage, one
// job or a batch per pop, report each task's Outcome, Reason and Result (R
// as JSON) to FinishTask.
func TestWorkflowDurableEnds(t *testing.T) {
	type storage interface {
		IPersistentQueue
		WorkflowAcknowledgeable
	}
	sources := map[string]func(*endRec) storage{
		"single": func(r *endRec) storage {
			return endStorage{mocks.NewMockPersistentQueue(), wfStorage{"q-e", "s"}, r}
		},
		"batch": func(r *endRec) storage {
			s := endBatchStorage{&batchTestQueue{}, wfStorage{"q-e", "s"}, r}
			var _ IBatchAcknowledgeable = s
			return s
		},
	}
	boom := errors.New("boom")
	workers := map[string]func(storage){
		"result": func(s storage) {
			NewResultWorker(func(j Job[int]) (string, error) {
				if j.Data() == 2 {
					return "", boom
				}
				return fmt.Sprint("r-", j.Data()), nil
			}).WithQueue(s)
		},
		"err": func(s storage) {
			NewErrWorker(func(j Job[int]) error {
				if j.Data() == 2 {
					return boom
				}
				return nil
			}).WithQueue(s)
		},
	}

	for sName, newStorage := range sources {
		for wName, bind := range workers {
			t.Run(sName+"/"+wName, func(t *testing.T) {
				rec := &endRec{ends: map[string]TaskEnd{}}
				s := newStorage(rec)
				require.True(t, s.Enqueue(taskBytes(t, "e2e", "ok", 1)))
				require.True(t, s.Enqueue(taskBytes(t, "e2e", "bad", 2)))
				bind(s) // binding pulls what the storage holds

				require.Eventually(t, func() bool { return len(rec.get()) == 2 }, 5*time.Second, time.Millisecond)
				ends := rec.get()

				var okResult []byte
				if wName == "result" {
					okResult = []byte(`"r-1"`)
				}
				assert.Equal(t, TaskEnd{Workflow: "e2e", Task: "ok", Outcome: OutcomeCompleted, Result: okResult}, ends["ok"])
				assert.Equal(t, TaskEnd{Workflow: "e2e", Task: "bad", Outcome: OutcomeFailed, Reason: "boom"}, ends["bad"])
				if b, ok := s.(endBatchStorage); ok {
					assert.Positive(t, b.pops.Load(), "popped through the batch source")
				}
			})
		}
	}
}

// Edge paths of workflows: failing stores, odd queues, misuse.

// badItemQueue is a user TaskQueue whose items fail to build.
type badItemQueue struct{}

func (badItemQueue) WorkflowItem(chan int) (WorkflowItem, error) {
	return WorkflowItem{target: taskTarget{queueID: "q-bad", scope: "s"},
		build: func(string, taskTag) (any, error) { return nil, errors.New("build boom") }}, nil
}

// TestWorkflowPlanErrors: a failing build, data that does not marshal and
// a second Undo all fail the Submit.
func TestWorkflowPlanErrors(t *testing.T) {
	wf := NewWorkflow(fakeWFStore{scope: "s"}, WithWorkflowID("pe"))
	wf.Add("a", badItemQueue{}, make(chan int)).Undo(badItemQueue{}, make(chan int)).Undo(badItemQueue{}, nil)

	err := wf.Submit(context.Background())
	require.Error(t, err)
	for _, want := range []string{
		`task "a": build boom`, `task "a" undo: build boom`,
		`task "a": json: unsupported type`, `task "a" undo: json: unsupported type`,
		`task "a": Undo set twice`,
	} {
		assert.Contains(t, err.Error(), want)
	}
}

// storeFuncs is a WorkflowStore with Cancel and Record set per test.
type storeFuncs struct {
	fakeWFStore
	cancel func() error
	record func() (WorkflowRecord, error)
}

func (s storeFuncs) Cancel(context.Context, string, *bool) error { return s.cancel() }
func (s storeFuncs) Record(context.Context, string) (WorkflowRecord, error) {
	return s.record()
}

func TestWorkflowStoreErrors(t *testing.T) {
	t.Run("Cancel returns the store's error", func(t *testing.T) {
		s := storeFuncs{cancel: func() error { return errors.New("redis down") }}
		assert.EqualError(t, OpenWorkflow(s, "w").Cancel(context.Background()), "redis down")
	})

	t.Run("Wait ends when ctx ends between polls", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		s := storeFuncs{record: func() (WorkflowRecord, error) {
			cancel() // done before the poll's sleep
			return WorkflowRecord{State: WorkflowRunning}, nil
		}}
		_, err := OpenWorkflow(s, "w").Wait(ctx)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("appendNew keeps one of each", func(t *testing.T) {
		assert.Equal(t, []string{"a"}, appendNew([]string{"a"}, "a"))
	})
}

// plainJob is a Job[int] that is no varmq job.
type plainJob struct{}

func (plainJob) ID() string               { return "p" }
func (plainJob) Data() int                { return 0 }
func (plainJob) Context() context.Context { return context.Background() }

func TestTaskContextErrors(t *testing.T) {
	assert.Nil(t, TaskOf[int](plainJob{}), "not a varmq job")

	var v int
	tc := &TaskContext{WorkflowID: "w", Name: "a"}
	assert.ErrorIs(t, tc.Result("b", &v), ErrNoResult, "no store")

	tc.store = storeFuncs{record: func() (WorkflowRecord, error) { return WorkflowRecord{}, errors.New("redis down") }}
	assert.EqualError(t, tc.Result("b", &v), "redis down")

	tc.store = storeFuncs{record: func() (WorkflowRecord, error) {
		return WorkflowRecord{Tasks: []TaskRecord{{Name: "b"}}}, nil
	}}
	assert.ErrorIs(t, tc.Result("b", &v), ErrNoResult, "the record lacks the task")
}

// TestLocalPokers: a poke reaches the source's ready hint; an unbound
// source leaves localPokers; a collected poker is dropped.
func TestLocalPokers(t *testing.T) {
	w := NewWorker(func(Job[int]) {}).(*workerBinder[int])
	defer w.Stop()
	src := w.bind(endBatchStorage{&batchTestQueue{}, wfStorage{"q-pokers", "s"}, &endRec{ends: map[string]TaskEnd{}}}, 0)
	defer queueRegistry.Delete("q-pokers")

	b, ok := src.(*batchAckSource[int, iJob[int]])
	require.True(t, ok)
	n := b.pokes.Load()
	pokeLocal([]string{"q-pokers"})
	assert.Equal(t, n+1, b.pokes.Load(), "the source's own poke")

	size := func(id string) int {
		set, _ := localPokers.Load(id)
		c := 0
		set.(*sync.Map).Range(func(any, any) bool { c++; return true })
		return c
	}
	w.removeSource(src)
	_, ok = w.pokers.Load(src)
	assert.False(t, ok)
	assert.Zero(t, size("q-pokers"))

	set, _ := localPokers.LoadOrStore("q-dead", &sync.Map{})
	set.(*sync.Map).Store(weak.Make(&queuePoker{id: "q-dead", poke: func() {}}), struct{}{})
	require.Eventually(t, func() bool {
		runtime.GC()
		pokeLocal([]string{"q-dead"})
		return size("q-dead") == 0
	}, 2*time.Second, time.Millisecond)
}

// plainPriorityQueue is custom priority storage with no force path.
type plainPriorityQueue struct{ IPriorityQueue }

func TestWorkflowTargets(t *testing.T) {
	t.Run("in-memory queue storage that counts itself", func(t *testing.T) {
		_, err := memWorkflowItem[int](&externalBaseQueue{q: queues.NewQueue[any]()}, false, nil, nil)
		assert.ErrorContains(t, err, "can't take workflow tasks")
	})

	t.Run("in-memory push refuses a non-job", func(t *testing.T) {
		it, err := NewWorker(func(Job[int]) {}, WithAutoRun(false)).BindQueue().WorkflowItem(1)
		require.NoError(t, err)
		assert.False(t, it.target.queue.(memTarget).push("not a job", 0))
	})

	t.Run("custom priority storage takes a root", func(t *testing.T) {
		w := NewWorker(func(Job[int]) {}, WithAutoRun(false))
		h := w.WithPriorityQueue(plainPriorityQueue{queues.NewPriorityQueue[iJob[int]]()})
		wf := NewWorkflow(NewWorkflowStore(), WithWorkflowID("pq"))
		wf.Add("a", h, 1)
		require.NoError(t, wf.Submit(context.Background()))
		assert.Equal(t, 1, h.Len())
	})

	t.Run("durable storage that can't finish tasks names its queue", func(t *testing.T) {
		_, err := durableWorkflowItem(struct {
			*mocks.MockPersistentQueue
			plainInspector
		}{mocks.NewMockPersistentQueue(), plainInspector{}}, false, 1)
		assert.ErrorIs(t, err, ErrScopeMismatch)
		assert.ErrorContains(t, err, "queue custom:plain can't take workflow tasks")
	})
}

func TestFailReasonNone(t *testing.T) {
	_, ok := failReason(&ackExt{})
	assert.False(t, ok, "no reason, no failed end")
	_, ok = failReason(&ackExt{end: &TaskEnd{Outcome: OutcomeCompleted}})
	assert.False(t, ok)
}

// TestWorkflowResultNotJSON: a result worker's task whose R does not
// marshal fails.
func TestWorkflowResultNotJSON(t *testing.T) {
	w := NewResultWorker(func(Job[int]) (chan int, error) { return make(chan int), nil })
	defer w.Stop()
	wf := NewWorkflow(NewWorkflowStore(), WithWorkflowID("rj"))
	wf.Add("a", w.BindQueue(), 1)
	require.NoError(t, wf.Submit(context.Background()))

	r := waitWF(t, wf)
	assert.Equal(t, WorkflowFailed, r.State)
	assert.Equal(t, JobFailed, r.Tasks[0].State)
	assert.Contains(t, r.Tasks[0].Reason, "varmq: task result: json: unsupported type")
}

// skipStorage is durable storage whose store stops every task and that
// refuses every FinishTask (wfStorage's).
type skipStorage struct {
	*mocks.MockPersistentQueue
	wfStorage
}

func (skipStorage) WorkflowStore() WorkflowStore { return notRunnable{fakeWFStore{scope: "s"}} }

type notRunnable struct{ fakeWFStore }

func (notRunnable) Runnable(context.Context, string, string, bool) (bool, error) {
	return false, nil
}

// TestWorkflowSkipAckFails: a task its workflow no longer wants ends
// cancelled; a refused ack of that end reaches the error handler.
func TestWorkflowSkipAckFails(t *testing.T) {
	errs := make(chan error, 4)
	ran := make(chan struct{}, 1)
	w := NewWorker(func(Job[int]) { ran <- struct{}{} }, WithErrHandler(func(_ Worker, err error) {
		select {
		case errs <- err:
		default:
		}
	}))
	defer w.Stop()

	s := skipStorage{mocks.NewMockPersistentQueue(), wfStorage{"q-skip", "s"}}
	defer queueRegistry.Delete("q-skip")
	require.True(t, s.Enqueue(taskBytes(t, "sk", "a", 1)))
	w.WithPersistentQueue(s)

	select {
	case err := <-errs:
		assert.ErrorIs(t, err, ErrAcknowledgeJob)
	case <-time.After(2 * time.Second):
		t.Fatal("refused ack never reported")
	}
	assert.Empty(t, ran, "never ran")
}

// cancelThenFail fails every read once a Cancel went through.
type cancelThenFail struct{ *fakeWorkflowStore }

func (c cancelThenFail) Cancel(ctx context.Context, id string, undo *bool) error {
	err := c.fakeWorkflowStore.Cancel(ctx, id, undo)
	c.err = errors.New("redis down")
	return err
}

// TestMuxWorkflowCancelReadFails: a read failing after the cancel is 503.
func TestMuxWorkflowCancelReadFails(t *testing.T) {
	workflowRegistry.Clear()
	defer workflowRegistry.Clear()

	registerWorkflowStore(cancelThenFail{&fakeWorkflowStore{scope: "ctf", recs: []WorkflowRecord{{ID: "c1"}}}})
	rr := httptest.NewRecorder()
	NewServerMux().ServeHTTP(rr, httptest.NewRequest("PATCH", "/workflows/c1/actions/cancel", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Contains(t, rr.Body.String(), "redis down")
}

func TestMemPlanCheckUndo(t *testing.T) {
	s, _ := newMemStore(t)
	q := NewWorker(func(Job[int]) {}, WithAutoRun(false)).BindQueue()
	wf := NewWorkflow(s, WithWorkflowID("pc"))
	wf.Add("a", q, 1).Undo(q, 2)
	p, err := wf.plan()
	require.NoError(t, err)

	p.Tasks[0].Undo.Queue = nil
	err = s.Submit(context.Background(), p)
	assert.ErrorIs(t, err, ErrScopeMismatch)
	assert.ErrorContains(t, err, `task "a undo"`)
}
