package varmq

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
)

// storeConfig is a workflow store's settings (adr/0005, Config).
type storeConfig struct {
	resultLimit int
	retention   time.Duration
}

// StoreConfigFunc configures a workflow store.
type StoreConfigFunc func(*storeConfig)

// WithResultLimit caps a task's result at n bytes (default 64 KiB). A
// bigger result fails the task with ErrResultTooLarge; store an
// object-store key instead.
func WithResultLimit(n int) StoreConfigFunc {
	return func(c *storeConfig) { c.resultLimit = n }
}

// WithWorkflowRetention sets how long an ended workflow is kept (default 7
// days). Running workflows are kept until they end.
func WithWorkflowRetention(d time.Duration) StoreConfigFunc {
	return func(c *storeConfig) { c.retention = d }
}

// refusedPromotion is the reason of a task whose storage refused its push.
const refusedPromotion = "queue refused promotion"

// memWorkflowStore is the workflow store of in-memory queues: Go maps under
// one mutex, lost on exit like the queues. Each workflow runs a wfState;
// the store applies its effects. Pushes run after the lock is released: a
// push wakes a worker, which may come back into the store.
type memWorkflowStore struct {
	cfg storeConfig
	now func() time.Time // a field so tests move the clock

	mu    sync.Mutex
	wfs   map[string]*memWorkflow
	order []*memWorkflow // Submit order; swept ones stay until compacted
	ended []*memWorkflow // end order, so the first one ended first
	gone  int            // swept workflows still in order
}

type memWorkflow struct {
	id, hash string
	s        *wfState
	created  int64 // Unix ms
	finished int64 // Unix ms; 0 while running or compensating
	gone     bool  // swept: only order still holds it
}

// memPush is one push, taken from the plan under the lock and run after it.
type memPush struct {
	w      *memWorkflow
	task   string
	undo   bool
	target memTarget
	item   memTaskItem
	prio   int
}

// memTaskItem is what the store needs of a task's job: every job kind has it.
type memTaskItem interface {
	taskTag() *taskTag
	Close() error
}

// NewWorkflowStore returns the workflow store of in-memory queues. Its
// scope is "memory:<InstanceID>", so a workflow may span every in-memory
// queue of the process. It is also a WorkflowInspector.
func NewWorkflowStore(opts ...StoreConfigFunc) WorkflowStore {
	c := storeConfig{resultLimit: 64 << 10, retention: 7 * 24 * time.Hour}
	for _, o := range opts {
		o(&c)
	}

	m := &memWorkflowStore{cfg: c, now: time.Now, wfs: map[string]*memWorkflow{}}
	registerWorkflowStore(m)

	return m
}

var _ WorkflowInspector = (*memWorkflowStore)(nil)

func (m *memWorkflowStore) ScopeID() string { return memoryScope() }

// Submit stores the plan and pushes its roots. A plan with a stored ID is
// success when its Hash is equal, else ErrWorkflowExists. A root its
// storage refuses fails like any refused push.
func (m *memWorkflowStore) Submit(_ context.Context, p WorkflowPlan) error {
	if err := memPlanCheck(p); err != nil {
		return err
	}

	m.mu.Lock()
	m.sweep()
	if w, ok := m.wfs[p.ID]; ok {
		m.mu.Unlock()
		if w.hash == p.Hash {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrWorkflowExists, p.ID)
	}

	for _, tp := range p.Tasks {
		tp.Item.(memTaskItem).taskTag().store = m
		if tp.Undo != nil {
			tp.Undo.Item.(memTaskItem).taskTag().store = m
		}
	}
	st, roots := newWFState(p)
	w := &memWorkflow{id: p.ID, hash: p.Hash, s: st, created: m.now().UnixMilli()}
	m.wfs[p.ID] = w
	m.order = append(m.order, w)
	pushes := m.effects(w, roots)
	m.mu.Unlock()

	m.push(pushes)

	return nil
}

// memPlanCheck refuses an empty plan and one with a task (or undo) that is
// not an in-memory queue's.
func memPlanCheck(p WorkflowPlan) error {
	check := func(name, queueID string, q, item any) error {
		_, mem := q.(memTarget)
		tagged, ok := item.(memTaskItem)
		if !mem || !ok || tagged.taskTag() == nil {
			return fmt.Errorf("%w: task %q queue %s is not in-memory", ErrScopeMismatch, name, queueID)
		}
		return nil
	}

	if len(p.Tasks) == 0 { // would run forever: no root, no end
		return fmt.Errorf("varmq: workflow %s has no tasks", p.ID)
	}
	for _, tp := range p.Tasks {
		if err := check(tp.Name, tp.QueueID, tp.Queue, tp.Item); err != nil {
			return err
		}
		if u := tp.Undo; u != nil {
			if err := check(tp.Name+" undo", u.QueueID, u.Queue, u.Item); err != nil {
				return err
			}
		}
	}

	return nil
}

// sweep drops workflows ended more than retention ago. Callers hold mu.
// ponytail: runs on each Submit, O(swept); a timer if idle stores must
// free memory.
func (m *memWorkflowStore) sweep() {
	cut := m.now().Add(-m.cfg.retention).UnixMilli()
	n := 0
	for ; n < len(m.ended) && m.ended[n].finished <= cut; n++ {
		w := m.ended[n]
		delete(m.wfs, w.id)
		w.gone = true
	}
	clear(m.ended[:n])
	m.ended = m.ended[n:]

	if m.gone += n; m.gone > len(m.order)/2 {
		m.order = slices.DeleteFunc(m.order, func(w *memWorkflow) bool { return w.gone })
		m.gone = 0
	}
}

// effects records an end and turns pushes into memPushes. Callers hold mu.
func (m *memWorkflowStore) effects(w *memWorkflow, fx []wfEffect) []memPush {
	var out []memPush
	for _, e := range fx {
		if e.End != "" {
			w.finished = m.now().UnixMilli()
			m.ended = append(m.ended, w)
		}
		if e.Push == "" {
			continue
		}

		tp := w.s.Tasks[e.Push].Plan
		p := memPush{w: w, task: e.Push, undo: e.Undo,
			target: tp.Queue.(memTarget), item: tp.Item.(memTaskItem), prio: tp.Priority}
		if e.Undo {
			p.target, p.item, p.prio = tp.Undo.Queue.(memTarget), tp.Undo.Item.(memTaskItem), tp.Undo.Priority
		}
		out = append(out, p)
	}

	return out
}

// push runs the pushes and returns the queue IDs pushed into. A refused
// push (custom storage without a force path) fails its task and closes the
// job, so its waiters release; the effects of that failure run too.
func (m *memWorkflowStore) push(work []memPush) []string {
	var pushed []string
	for len(work) > 0 {
		p := work[0]
		work = work[1:]
		if p.target.push(p.item, p.prio) {
			pushed = append(pushed, p.target.queueID)
			continue
		}

		m.mu.Lock()
		// The task is in the plan and the outcome known: no error.
		fx, _ := p.w.s.finish(TaskEnd{Workflow: p.w.id, Task: p.task, Outcome: OutcomeFailed,
			Reason: refusedPromotion, Undo: p.undo})
		work = append(work, m.effects(p.w, fx)...)
		m.mu.Unlock()

		// After the finish, so a Close that reports the task finds it ended.
		_ = p.item.Close()
	}

	return pushed
}

// finish records a task's (or undo's) outcome and pushes what it releases.
// It returns the queue IDs pushed into. Only a completed task keeps a
// result; one over the limit fails the task with ErrResultTooLarge.
func (m *memWorkflowStore) finish(e TaskEnd) (pushed []string, err error) {
	switch {
	case e.Outcome != OutcomeCompleted || e.Undo:
		e.Result = nil
	case len(e.Result) > m.cfg.resultLimit:
		e.Outcome, e.Reason, e.Result = OutcomeFailed, ErrResultTooLarge.Error(), nil
	default:
		e.Result = bytes.Clone(e.Result) // the caller's buffer may be reused
	}

	m.mu.Lock()
	w, err := m.get(e.Workflow)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	fx, err := w.s.finish(e)
	pushes := m.effects(w, fx)
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	return m.push(pushes), nil
}

// get returns workflow id. Callers hold mu.
func (m *memWorkflowStore) get(id string) (*memWorkflow, error) {
	w, ok := m.wfs[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrWorkflowNotFound, id)
	}

	return w, nil
}

// Cancel cancels waiting tasks now; queued ones are cancelled by the
// pre-run check. Compensation runs once nothing is pending if undo says so,
// or (undo nil) if the workflow was submitted to. Cancel of an ended
// workflow does nothing.
func (m *memWorkflowStore) Cancel(_ context.Context, id string, undo *bool) error {
	m.mu.Lock()
	w, err := m.get(id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	on := w.s.UndoOnCancel
	if undo != nil {
		on = *undo
	}
	pushes := m.effects(w, w.s.cancel(on))
	m.mu.Unlock()

	m.push(pushes)

	return nil
}

// Runnable is the pre-run check: whether the task (undo: its undo) may
// start now.
func (m *memWorkflowStore) Runnable(_ context.Context, workflowID, task string, undo bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, err := m.get(workflowID)
	if err != nil {
		return false, err
	}

	return w.s.runnable(task, undo), nil
}

// DepResult returns a copy of task's result: ErrDepFailed when it failed or
// was cancelled, ErrNoResult when it left none, has not ended or is not a
// task of the workflow.
func (m *memWorkflowStore) DepResult(_ context.Context, workflowID, task string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, err := m.get(workflowID)
	if err != nil {
		return nil, err
	}

	t, ok := w.s.Tasks[task]
	switch {
	case ok && (t.State == JobFailed || t.State == JobCancelled):
		return nil, ErrDepFailed
	case !ok || t.State != JobCompleted || len(t.Result) == 0:
		return nil, ErrNoResult
	}

	return bytes.Clone(t.Result), nil
}

// Record returns the workflow as the store holds it.
func (m *memWorkflowStore) Record(_ context.Context, id string) (WorkflowRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, err := m.get(id)
	if err != nil {
		return WorkflowRecord{}, err
	}

	return w.record(), nil
}

// Workflows lists up to limit workflows in state ("" for every state),
// newest first, skipping the first offset. Records are built after the
// listing, under a short lock each, so a big page never holds mu long.
func (m *memWorkflowStore) Workflows(_ context.Context, state WorkflowState, offset, limit int) ([]WorkflowRecord, error) {
	ws := m.list(state, offset, limit)
	out := make([]WorkflowRecord, 0, len(ws))
	for _, w := range ws {
		m.mu.Lock()
		out = append(out, w.record())
		m.mu.Unlock()
	}

	return out, nil
}

// WorkflowKeys is Workflows without the records.
func (m *memWorkflowStore) WorkflowKeys(_ context.Context, state WorkflowState, offset, limit int) ([]WorkflowKey, error) {
	ws := m.list(state, offset, limit)
	out := make([]WorkflowKey, len(ws))
	for i, w := range ws {
		out[i] = WorkflowKey{ID: w.id, CreatedAt: w.created} // fixed at Submit: no lock
	}

	return out, nil
}

// list collects the listed workflows under mu, in the order GET /workflows
// merges: CreatedAt newest first, then ID. A state filter scans at most
// maxWorkflowsOffset+limit workflows, matched or not, so a rare state never
// walks the whole store.
// ponytail: walks the kept workflows; an index per state if listing big
// stores shows up.
func (m *memWorkflowStore) list(state WorkflowState, offset, limit int) []*memWorkflow {
	offset, limit = max(offset, 0), max(limit, 0)
	want := offset + min(limit, math.MaxInt-offset)
	var out []*memWorkflow

	m.mu.Lock()
	defer m.mu.Unlock()

	for i := len(m.order) - 1; i >= 0; i-- {
		if scanned := len(m.order) - 1 - i; state != "" && scanned-limit >= maxWorkflowsOffset {
			break
		}
		w := m.order[i]
		if w.gone || state != "" && w.s.State != state {
			continue
		}
		// Submit order is CreatedAt order: past want, only the last one's
		// ties (same ms) may still sort into the page.
		if len(out) >= want && w.created != out[len(out)-1].created {
			break
		}
		out = append(out, w)
	}

	slices.SortFunc(out, func(a, b *memWorkflow) int {
		return cmp.Or(cmp.Compare(b.created, a.created), strings.Compare(a.id, b.id))
	})

	return out[min(offset, len(out)):min(want, len(out))]
}

// record builds w's WorkflowRecord: tasks in Add order. An undo is waiting
// until compensation reaches it. Callers hold mu.
func (w *memWorkflow) record() WorkflowRecord {
	r := WorkflowRecord{ID: w.id, State: w.s.State, CreatedAt: w.created, FinishedAt: w.finished,
		UndoOnCancel: w.s.UndoOnCancel, Tasks: make([]TaskRecord, 0, len(w.s.Order))}

	for _, name := range w.s.Order {
		t := w.s.Tasks[name]
		tr := TaskRecord{Name: name, State: t.State, Deps: slices.Clone(t.Plan.Deps), QueueID: t.Plan.QueueID,
			JobID: t.Plan.JobID, Always: t.Plan.Always, Reason: t.Reason, ResultSize: len(t.Result)}
		if u := t.Plan.Undo; u != nil {
			st := t.UndoState
			if st == "" {
				st = JobWaiting
			}
			tr.Undo = &TaskRecord{Name: name, State: st, QueueID: u.QueueID, JobID: u.JobID}
		}
		r.Tasks = append(r.Tasks, tr)
	}

	return r
}
