package varmq

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

// ErrWorkflowSubmitted: the workflow is submitted (or opened by ID), so its
// DAG can't change.
var ErrWorkflowSubmitted = errors.New("varmq: workflow already submitted")

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// validName: 1 to max characters of [A-Za-z0-9_.-].
func validName(s string, max int) bool { return len(s) <= max && nameRe.MatchString(s) }

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
	opened    bool // from OpenWorkflow: a handle, not a builder
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
		wf.errs = append(wf.errs, fmt.Errorf("%w: workflow %s: Add %q", ErrWorkflowSubmitted, wf.id, name))
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
		t.wf.errs = append(t.wf.errs, fmt.Errorf("%w: task %q: %s", ErrWorkflowSubmitted, t.name, op))
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

// validWorkflowID: `[A-Za-z0-9_.-]{1,128}`, not "index" (the index key).
func validWorkflowID(id string) bool { return validName(id, 128) && id != "index" }

// validTaskName: `[A-Za-z0-9_.-]{1,64}`.
func validTaskName(name string) bool { return validName(name, 64) }

// taskJobID is a task's job ID; its undo's adds ":undo".
func taskJobID(wf, task string) string { return "wf:" + wf + ":" + task }

// plan validates the workflow and builds what the store submits. Items are
// built now, so a durable envelope's enqueued_at is the Submit time.
func (wf *Workflow) plan() (WorkflowPlan, error) {
	errs := slices.Clone(wf.errs)
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if !validWorkflowID(wf.id) {
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
		case it.build == nil: // a zero WorkflowItem from a user TaskQueue
			add("varmq: task %q: queue gave no workflow item", task)
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
		if !validTaskName(t.name) {
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

// OpenWorkflow returns a handle to the submitted workflow id: Cancel,
// Status and Wait work; Add and Submit fail with ErrWorkflowSubmitted. An
// invalid id gives a handle whose ops fail with ErrWorkflowNotFound.
func OpenWorkflow(store WorkflowStore, id string) *Workflow {
	return &Workflow{id: id, store: store, submitted: true, opened: true}
}

var errNoWorkflowStore = errors.New("varmq: workflow has no store")

// handleErr is why Cancel, Status and Wait can't ask the store: none, or
// an ID no store can hold.
func (wf *Workflow) handleErr() error {
	switch {
	case wf.store == nil:
		return errNoWorkflowStore
	case !validWorkflowID(wf.id):
		return fmt.Errorf("%w: invalid workflow id %q", ErrWorkflowNotFound, wf.id)
	}

	return nil
}

// Submit validates and stores the workflow, then queues its roots. Safe to
// repeat: the same ID and shape succeed again. A failed Submit leaves wf
// unsubmitted, so the caller can retry.
func (wf *Workflow) Submit(ctx context.Context) error {
	if wf.opened {
		return ErrWorkflowSubmitted
	}

	// plan each time: it stamps enqueued_at, so never reuse one.
	p, err := wf.plan()
	if err != nil {
		return err
	}

	// Shared storage wakes only its subscribed consumers. Woken on error
	// too: the store may have committed before the reply was lost.
	var roots []string
	for _, tp := range p.Tasks {
		if len(tp.Deps) == 0 {
			roots = appendNew(roots, tp.QueueID)
		}
	}
	err = wf.store.Submit(ctx, p)
	pokeLocal(roots)
	if err != nil {
		return err
	}
	wf.submitted = true

	return nil
}

// Cancel cancels the workflow; undos run if it was submitted with
// WithUndoOnCancel(true), the default. The store reads that from the
// workflow, so a handle from OpenWorkflow cancels it the same way.
func (wf *Workflow) Cancel(ctx context.Context) error {
	if err := wf.handleErr(); err != nil {
		return err
	}
	if err := wf.store.Cancel(ctx, wf.id, nil); err != nil {
		return err
	}

	// Wake local consumers of the undos the cancel may have pushed.
	var ids []string
	if !wf.opened {
		for _, t := range wf.tasks {
			if t.undo != nil && t.undo.err == nil {
				ids = appendNew(ids, t.undo.item.target.queueID)
			}
		}
	} else if r, err := wf.store.Record(ctx, wf.id); err == nil {
		// ponytail: a failed read skips the wake; consumers see the undos
		// at their next resync.
		ids = undoQueueIDs(r)
	}
	pokeLocal(ids)

	return nil
}

// undoQueueIDs is the queue ID of every undo of r, each once.
func undoQueueIDs(r WorkflowRecord) []string {
	var ids []string
	for _, t := range r.Tasks {
		if t.Undo != nil {
			ids = appendNew(ids, t.Undo.QueueID)
		}
	}

	return ids
}

// appendNew appends id unless ids has it. Workflows span few queues.
func appendNew(ids []string, id string) []string {
	if slices.Contains(ids, id) {
		return ids
	}

	return append(ids, id)
}

// Status returns the workflow's current record.
func (wf *Workflow) Status(ctx context.Context) (WorkflowRecord, error) {
	if err := wf.handleErr(); err != nil {
		return WorkflowRecord{}, err
	}

	return wf.store.Record(ctx, wf.id)
}

func isFinal(s WorkflowState) bool { return s != WorkflowRunning && s != WorkflowCompensating }

// Wait blocks until the workflow reaches a final state; a store read error
// returns early.
func (wf *Workflow) Wait() (WorkflowRecord, error) { return wf.WaitContext(context.Background()) }

// WaitContext is Wait that gives up when ctx ends: ctx.Err() then.
func (wf *Workflow) WaitContext(ctx context.Context) (WorkflowRecord, error) {
	if err := wf.handleErr(); err != nil {
		return WorkflowRecord{}, err
	}

	// ponytail: polling; a store push later
	for {
		if err := ctx.Err(); err != nil {
			return WorkflowRecord{}, err
		}
		r, err := wf.store.Record(ctx, wf.id)
		if err != nil || isFinal(r.State) {
			return r, err
		}
		select {
		case <-ctx.Done():
		case <-time.After(50 * time.Millisecond):
		}
	}
}
