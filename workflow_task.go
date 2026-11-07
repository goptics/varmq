package varmq

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
)

// TaskContext is a workflow task's view of its workflow, for its worker.
// Get one with TaskOf. Safe for concurrent use.
type TaskContext struct {
	WorkflowID string
	Name       string
	Undo       bool // the task's undo job, not the task

	store WorkflowStore

	mu    sync.Mutex
	tasks map[string]TaskRecord // fetched once on first use
}

// TaskOf is j's workflow task, nil when j is not one. An undo job gets a
// context too, with Undo set.
func TaskOf[T any](j Job[T]) *TaskContext {
	ij, ok := j.(iJob[T])
	if !ok {
		return nil
	}

	tag := ij.taskTag()
	if tag == nil {
		return nil
	}

	return &TaskContext{WorkflowID: tag.wf, Name: tag.task, Undo: tag.undo, store: tag.store}
}

// deps is this task's dependency names and the workflow's tasks by name,
// fetched once and cached (never changed after). A failed fetch is not
// cached, so the next call retries.
// ponytail: context.Background, as runnable: no ctx on the accessor. The
// snapshot is of when it was first read; deps are final by then (task ran).
func (t *TaskContext) deps() ([]string, map[string]TaskRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tasks == nil {
		if t.store == nil {
			return nil, nil, ErrNoResult
		}
		r, err := t.store.Record(context.Background(), t.WorkflowID)
		if err != nil {
			return nil, nil, err
		}
		t.tasks = make(map[string]TaskRecord, len(r.Tasks))
		for _, tr := range r.Tasks {
			t.tasks[tr.Name] = tr
		}
	}

	self, ok := t.tasks[t.Name]
	if !ok {
		return nil, nil, ErrNoResult
	}

	return self.Deps, t.tasks, nil
}

// Result unmarshals dep's result into v. ErrNoResult when dep is not one of
// this task's dependencies (an undo may also read its own task) or left no
// result; ErrDepFailed when dep failed or was cancelled.
func (t *TaskContext) Result(dep string, v any) error {
	deps, _, err := t.deps()
	if err != nil {
		return err
	}

	if !slices.Contains(deps, dep) && !(t.Undo && dep == t.Name) {
		return ErrNoResult
	}

	b, err := t.store.DepResult(context.Background(), t.WorkflowID, dep)
	if err != nil {
		return err
	}

	return json.Unmarshal(b, v)
}

// Failed reports whether dep, one of this task's dependencies, failed or was
// cancelled. False for a non-dependency or when the record is unreadable.
func (t *TaskContext) Failed(dep string) bool {
	deps, tasks, err := t.deps()
	if err != nil || !slices.Contains(deps, dep) {
		return false
	}

	st := tasks[dep].State
	return st == JobFailed || st == JobCancelled
}
