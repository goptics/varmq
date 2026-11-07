package varmq

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
)

var (
	taskNameRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	workflowIDRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
)

// Workflow is a DAG of tasks, each a job bound for a queue (adr/0005). Build
// it with Add and the Task methods; the DAG is fixed at Submit. Builder
// misuse is recorded and returned by Submit.
type Workflow struct {
	id        string
	store     WorkflowStore
	opts      workflowConfig
	tasks     []*Task
	errs      []error
	submitted bool
}

// Task is one named job of a Workflow. Its methods return the same *Task.
type Task struct {
	name   string
	wf     *Workflow
	index  int // in wf.tasks; -1 when not added
	item   WorkflowItem
	err    error // the queue can't take it
	data   any
	deps   []*Task
	always bool
	prio   *int
	undo   *undoSpec
}

// undoSpec is a task's compensation job.
type undoSpec struct {
	item WorkflowItem
	err  error
	data any
}

type workflowConfig struct {
	id           string
	failFast     bool
	undoOnCancel bool
}

// WorkflowConfigFunc configures a Workflow.
type WorkflowConfigFunc func(*workflowConfig)

// WithWorkflowID sets the workflow ID, `[A-Za-z0-9_.-]{1,128}`, not
// "index". Submit is idempotent per ID. Generated when not given.
func WithWorkflowID(id string) WorkflowConfigFunc {
	return func(c *workflowConfig) { c.id = id }
}

// WithFailFast cancels every waiting task at the first unabsorbed failure,
// instead of letting unrelated branches finish.
func WithFailFast() WorkflowConfigFunc {
	return func(c *workflowConfig) { c.failFast = true }
}

// WithUndoOnCancel sets whether Cancel runs undos (default true).
func WithUndoOnCancel(on bool) WorkflowConfigFunc {
	return func(c *workflowConfig) { c.undoOnCancel = on }
}

// NewWorkflow starts a workflow whose state store keeps. Every queue of it
// must be in store's Atomic Scope.
func NewWorkflow(store WorkflowStore, opts ...WorkflowConfigFunc) *Workflow {
	c := workflowConfig{undoOnCancel: true}
	for _, o := range opts {
		o(&c)
	}
	if c.id == "" {
		c.id = rand.Text()
	}

	return &Workflow{id: c.id, store: store, opts: c}
}

// ID returns the workflow ID.
func (wf *Workflow) ID() string { return wf.id }

// Add adds task name, a job with data bound for q. Task names are
// `[A-Za-z0-9_.-]{1,64}`, unique in the workflow; the job ID is
// "wf:<workflow id>:<name>".
func (wf *Workflow) Add[T any](name string, q TaskQueue[T], data T) *Task {
	t := &Task{name: name, wf: wf, index: -1, data: data}
	if wf.submitted {
		wf.errs = append(wf.errs, fmt.Errorf("varmq: workflow %s: Add %q after Submit", wf.id, name))
		return t
	}

	t.item, t.err = itemOf(q, data)
	t.index = len(wf.tasks)
	wf.tasks = append(wf.tasks, t)

	return t
}

// usable records an error and reports false when t can't change anymore.
func (t *Task) usable(op string) bool {
	if t.wf.submitted {
		t.wf.errs = append(t.wf.errs, fmt.Errorf("varmq: task %q: %s after Submit", t.name, op))
		return false
	}

	return t.index >= 0 // not added: Add recorded why
}

// After makes t wait for deps. Each dep must be a task of the same
// workflow added before t (Add order is a topological order, so no cycles).
func (t *Task) After(deps ...*Task) *Task {
	if !t.usable("After") {
		return t
	}

	for _, d := range deps {
		switch {
		case d == nil || d.wf != t.wf || d.index < 0:
			t.wf.errs = append(t.wf.errs, fmt.Errorf("varmq: task %q: After a task of another workflow", t.name))
		case d.index >= t.index:
			t.wf.errs = append(t.wf.errs, fmt.Errorf("varmq: task %q: After %q, which is not added before it", t.name, d.name))
		case !slices.Contains(t.deps, d):
			t.deps = append(t.deps, d)
		}
	}

	return t
}

// Always runs t even when a dependency failed or was cancelled (default:
// t is cancelled).
func (t *Task) Always() *Task {
	if t.usable("Always") {
		t.always = true
	}

	return t
}

// Undo registers t's compensation: a job with data bound for q, queued only
// if t completed and the workflow later failed. Job ID: t's plus ":undo".
func (t *Task) Undo[U any](q TaskQueue[U], data U) *Task {
	if !t.usable("Undo") {
		return t
	}
	if t.undo != nil {
		t.wf.errs = append(t.wf.errs, fmt.Errorf("varmq: task %q: Undo set twice", t.name))
		return t
	}

	u := &undoSpec{data: data}
	u.item, u.err = itemOf(q, data)
	t.undo = u

	return t
}

// Priority sets t's priority on a priority queue.
func (t *Task) Priority(p int) *Task {
	if !t.usable("Priority") {
		return t
	}
	if t.err == nil && !t.item.target.priority {
		t.wf.errs = append(t.wf.errs, fmt.Errorf("varmq: task %q: Priority on a FIFO queue", t.name))
		return t
	}

	t.prio = &p

	return t
}

// taskJobID is a task's job ID; its undo's adds ":undo".
func taskJobID(wf, task string) string { return "wf:" + wf + ":" + task }

// plan validates the workflow and builds what the store submits. Items are
// built now, so a durable envelope's enqueued_at is the Submit time.
func (wf *Workflow) plan() (WorkflowPlan, error) {
	errs := slices.Clone(wf.errs)
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if !workflowIDRe.MatchString(wf.id) || wf.id == "index" {
		add("varmq: invalid workflow id %q", wf.id)
	}
	if len(wf.tasks) == 0 {
		add("varmq: workflow %s has no tasks", wf.id)
	}

	scope := ""
	if wf.store == nil {
		add("varmq: workflow %s has no store", wf.id)
	} else {
		scope = wf.store.ScopeID()
	}

	// target checks a task's queue: usable and in the store's scope.
	target := func(task string, it WorkflowItem, err error) bool {
		tt := it.target
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("task %q: %w", task, err))
		case wf.store != nil && tt.scope != scope:
			add("%w: task %q queue %s is in %s, the store in %s", ErrScopeMismatch, task, tt.queueID, tt.scope, scope)
		default:
			return true
		}
		return false
	}

	p := WorkflowPlan{ID: wf.id, FailFast: wf.opts.failFast, UndoOnCancel: wf.opts.undoOnCancel,
		Tasks: make([]TaskPlan, 0, len(wf.tasks))}
	data := make([][]byte, len(wf.tasks))
	undoData := make([][]byte, len(wf.tasks))
	seen := make(map[string]bool, len(wf.tasks))

	for i, t := range wf.tasks {
		if !taskNameRe.MatchString(t.name) {
			add("varmq: invalid task name %q", t.name)
		} else if seen[t.name] {
			add("varmq: duplicate task name %q", t.name)
		}
		seen[t.name] = true

		tp := TaskPlan{Name: t.name, JobID: taskJobID(wf.id, t.name), QueueID: t.item.target.queueID,
			Queue: t.item.target.queue, Always: t.always}
		if t.prio != nil {
			tp.Priority, tp.HasPriority = *t.prio, true
		}
		for _, d := range t.deps {
			tp.Deps = append(tp.Deps, d.name)
		}

		var err error
		tag := taskTag{wf: wf.id, task: t.name}
		if target(t.name, t.item, t.err) {
			if tp.Item, err = t.item.build(tp.JobID, tag); err != nil {
				add("varmq: task %q: %w", t.name, err)
			}
		}
		if data[i], err = json.Marshal(t.data); err != nil {
			add("varmq: task %q: %w", t.name, err)
		}

		if u := t.undo; u != nil {
			up := &UndoPlan{JobID: tp.JobID + ":undo", QueueID: u.item.target.queueID, Queue: u.item.target.queue}
			if target(t.name+" undo", u.item, u.err) {
				undoTag := taskTag{wf: wf.id, task: t.name, undo: true}
				if up.Item, err = u.item.build(up.JobID, undoTag); err != nil {
					add("varmq: task %q undo: %w", t.name, err)
				}
			}
			if undoData[i], err = json.Marshal(u.data); err != nil {
				add("varmq: task %q undo: %w", t.name, err)
			}
			tp.Undo = up
		}

		p.Tasks = append(p.Tasks, tp)
	}

	if len(errs) > 0 {
		return WorkflowPlan{}, errors.Join(errs...)
	}

	p.Hash = planHash(p, data, undoData)

	return p, nil
}

// planHash is the hex SHA-256 of the plan's shape: per task in Add order its
// name, queue ID, priority, sorted deps, policy, undo queue ID and data, and
// data JSON (not the envelope: enqueued_at differs per Submit); then
// FailFast and UndoOnCancel. Fields are length-prefixed, so none can run
// into the next.
func planHash(p WorkflowPlan, data, undoData [][]byte) string {
	h := sha256.New()
	field := func(b []byte) {
		h.Write(binary.AppendUvarint(nil, uint64(len(b))))
		h.Write(b)
	}
	str := func(s string) { field([]byte(s)) }
	flag := func(b bool) {
		if b {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
	}
	num := func(n int) { h.Write(binary.AppendVarint(nil, int64(n))) }

	num(len(p.Tasks))
	for i, tp := range p.Tasks {
		str(tp.Name)
		str(tp.QueueID)
		flag(tp.HasPriority)
		num(tp.Priority)

		num(len(tp.Deps))
		for _, d := range slices.Sorted(slices.Values(tp.Deps)) {
			str(d)
		}

		flag(tp.Always)
		flag(tp.Undo != nil)
		if tp.Undo != nil {
			str(tp.Undo.QueueID)
			field(undoData[i])
		}
		field(data[i])
	}
	flag(p.FailFast)
	flag(p.UndoOnCancel)

	return hex.EncodeToString(h.Sum(nil))
}
