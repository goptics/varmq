package varmq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"
	"weak"

	"github.com/goptics/varmq/internal/queues"
	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
