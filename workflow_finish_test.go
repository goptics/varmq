package varmq

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
