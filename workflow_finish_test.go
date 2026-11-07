package varmq

import (
	"context"
	"errors"
	"fmt"
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

	var got []string
	mailQ := NewErrWorker(func(j Job[string]) error {
		log.add("email")
		for _, dep := range []string{"thumb-s", "thumb-l"} {
			b, err := store.DepResult(ctx, "dag", dep)
			if err != nil {
				return err
			}
			got = append(got, string(b))
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
	assert.Equal(t, []string{"640", "5120"}, got)

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
	return c.st.s.runnable(task, undo), nil
}

func (c crashStorage) WorkflowStore() WorkflowStore { return crashStore{memStore(), c.st} }

func (c crashStorage) FinishTask(_ string, e TaskEnd) ([]string, bool) {
	st := c.st
	st.mu.Lock()
	defer st.mu.Unlock()

	st.finishs++
	applied, lost := st.lose(st.finishs)
	var pushed []string
	if applied {
		fx, err := st.s.finish(e)
		if err != nil {
			return nil, false
		}
		for _, f := range fx {
			if f.Push == "b" {
				st.pushedB++
				st.bQueue.Queue.Enqueue(st.bItem) // no wake: FinishTask's caller pokes
				pushed = append(pushed, "q-b")
			}
		}
	}

	return pushed, !lost
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
		{"reply lost", true, 1, 2},
		{"crash before finish", false, 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := WorkflowPlan{ID: "cr", Tasks: []TaskPlan{{Name: "a"}, {Name: "b", Deps: []string{"a"}}}}
			st := &crashState{s: newWFState(plan), bQueue: mocks.NewMockPersistentQueue(),
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
			if !tc.applied { // the reply that pushed b came back: its consumer is poked
				require.Eventually(t, func() bool { return bRuns.Load() == 1 }, 5*time.Second, time.Millisecond)
				bw.Wait()
			}

			st.mu.Lock()
			defer st.mu.Unlock()
			assert.Equal(t, 1, st.pushedB, "b pushed once")
			assert.Equal(t, tc.finishs, st.finishs)
			assert.Equal(t, tc.aRuns, aRuns.Load())
			assert.Equal(t, JobCompleted, st.s.Tasks["a"].State)
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
		p := w.WithPersistentQueue(pq)
		require.True(t, p.Add(1))
		require.True(t, p.Add(2))
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
