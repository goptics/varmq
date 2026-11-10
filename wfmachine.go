package varmq

import (
	"fmt"
	"slices"
)

// wfState is one workflow's state machine (adr/0005, Lifecycle). Pure: no
// I/O, no locks; the caller holds the workflow's lock. It is the reference
// the stores follow, so every walk goes in Order, never map order.
type wfState struct {
	State    WorkflowState
	Tasks    map[string]*wfTask
	Order    []string // Add order = topological
	FailFast bool
	// UndoOnCancel: a cancel compensates. The plan's, until cancel sets
	// the value its caller resolved.
	UndoOnCancel bool
	// At is now, Unix ms: the store sets it before finish and cancel, which
	// stamp what they end. The store stamps pushes and starts itself.
	At int64

	next      map[string][]string // dependents per task, in Order
	failed    bool                // an unabsorbed failure happened before any cancel
	cancelled bool                // cancel was called
}

type wfTask struct {
	Plan       TaskPlan
	State      JobState
	Open       int // dependencies not ended yet
	Reason     string
	Result     []byte
	UndoState  JobState // "" none, waiting, pending, completed, failed
	UndoOpen   int      // undos of completed descendants not ended yet
	UndoReason string   // why the undo failed or was cancelled

	// Unix ms, 0 when not reached (TaskRecord).
	Enqueued, Started, Finished             int64
	UndoEnqueued, UndoStarted, UndoFinished int64
}

// wfEffect is one thing storage must do: push a task (or its undo), and/or
// end the workflow.
type wfEffect struct {
	Push string // task name
	Undo bool
	End  WorkflowState
}

// newWFState builds the start state: roots pending, others waiting on
// every dependency. It also returns the pushes Submit does: the roots.
func newWFState(p WorkflowPlan) (*wfState, []wfEffect) {
	var roots []wfEffect
	s := &wfState{
		State:        WorkflowRunning,
		Tasks:        make(map[string]*wfTask, len(p.Tasks)),
		Order:        make([]string, 0, len(p.Tasks)),
		FailFast:     p.FailFast,
		UndoOnCancel: p.UndoOnCancel,
		next:         make(map[string][]string, len(p.Tasks)),
	}
	for _, tp := range p.Tasks {
		t := &wfTask{Plan: tp, State: JobPending, Open: len(tp.Deps)}
		if t.Open > 0 {
			t.State = JobWaiting
		} else {
			roots = append(roots, wfEffect{Push: tp.Name})
		}
		s.Tasks[tp.Name] = t
		s.Order = append(s.Order, tp.Name)
		for _, d := range tp.Deps {
			s.next[d] = append(s.next[d], tp.Name)
		}
	}
	return s, roots
}

// finish applies a task's (or undo's) outcome. A finish for a task that is
// not pending or processing (waiting, cancelled, already ended) or for an
// ended workflow does nothing: re-sends and late finishes are safe.
func (s *wfState) finish(e TaskEnd) ([]wfEffect, error) {
	t, ok := s.Tasks[e.Task]
	if !ok {
		return nil, fmt.Errorf("varmq: workflow %q has no task %q", e.Workflow, e.Task)
	}
	switch e.Outcome {
	case OutcomeCompleted, OutcomeFailed, OutcomeCancelled:
	default:
		return nil, fmt.Errorf("varmq: workflow %q task %q: unknown outcome %q", e.Workflow, e.Task, e.Outcome)
	}
	if e.Undo {
		return s.finishUndo(t, e), nil
	}
	if s.State != WorkflowRunning || !active(t.State) {
		return nil, nil
	}

	t.Reason, t.Result, t.Finished = e.Reason, e.Result, s.At
	push := map[string]bool{}
	if e.Outcome == OutcomeCompleted {
		t.State = JobCompleted
		for _, d := range s.next[e.Task] {
			s.release(d, push)
		}
		return s.settle(push), nil
	}

	t.State = JobFailed
	if e.Outcome == OutcomeCancelled {
		t.State = JobCancelled
	}
	// After cancel, ends (pre-run cancels included) are not failures.
	if !s.cancelled && !s.absorbed(e.Task) {
		if s.FailFast && !s.failed { // first unabsorbed failure only
			s.failFast(e.Task, push)
		}
		s.failed = true
	}
	s.drop(e.Task, push)
	return s.settle(push), nil
}

// cancel cancels waiting tasks now. Pending ones stay: the pre-run check
// finishes them as cancelled. The workflow ends once nothing is pending.
// undo is the effective value: the store resolves a nil override to
// UndoOnCancel.
func (s *wfState) cancel(undo bool) []wfEffect {
	if s.State != WorkflowRunning || s.cancelled {
		return nil
	}
	s.cancelled, s.UndoOnCancel = true, undo
	s.cancelWaiting()
	return s.settle(nil)
}

// runnable is the pre-run check: may this task (undo: its undo) start now.
// Only a pending one may, and only while its phase is on. Once FailFast
// fired, only Always tasks start: queued others are stopped, and the failed
// task's kept Always dependents still run.
func (s *wfState) runnable(task string, undo bool) bool {
	t, ok := s.Tasks[task]
	if !ok {
		return false
	}
	if undo {
		return s.State == WorkflowCompensating && t.UndoState == JobPending
	}
	stopped := s.FailFast && s.failed && !t.Plan.Always
	return s.State == WorkflowRunning && !s.cancelled && !stopped && t.State == JobPending
}

func (s *wfState) finishUndo(t *wfTask, e TaskEnd) []wfEffect {
	if s.State != WorkflowCompensating || !active(t.UndoState) {
		return nil
	}
	t.UndoState, t.UndoReason, t.UndoFinished = JobFailed, e.Reason, s.At
	if e.Outcome == OutcomeCompleted {
		t.UndoState, t.UndoReason = JobCompleted, ""
	}
	push := map[string]bool{}
	up := s.reach(e.Task, true)
	for _, name := range s.Order {
		x := s.Tasks[name]
		if !up[name] || x.UndoState != JobWaiting {
			continue
		}
		if x.UndoOpen--; x.UndoOpen == 0 {
			x.UndoState = JobPending
			push[name] = true
		}
	}
	fx := s.emit(push, true)

	failed := false
	for _, name := range s.Order {
		switch s.Tasks[name].UndoState {
		case JobWaiting, JobPending, JobProcessing:
			return fx
		case JobFailed:
			failed = true
		}
	}
	end := WorkflowCompensated
	if failed {
		end = WorkflowCompensationFailed
	}
	return s.end(fx, end)
}

// release counts one ended dependency of a waiting task; pushes it at zero.
func (s *wfState) release(name string, push map[string]bool) {
	t := s.Tasks[name]
	if t.State != JobWaiting {
		return
	}
	if t.Open--; t.Open == 0 {
		t.State = JobPending
		push[name] = true
	}
}

// drop applies a failed or cancelled task to its dependents: Always ones
// count it as ended, others are cancelled, recursively.
func (s *wfState) drop(name string, push map[string]bool) {
	for _, d := range s.next[name] {
		t := s.Tasks[d]
		if t.State != JobWaiting {
			continue
		}
		if t.Plan.Always {
			s.release(d, push)
			continue
		}
		t.State, t.Finished = JobCancelled, s.At
		s.drop(d, push)
	}
}

// absorbed: the task has dependents and every one is Always.
func (s *wfState) absorbed(name string) bool {
	next := s.next[name]
	return len(next) > 0 && !slices.ContainsFunc(next, func(d string) bool { return !s.Tasks[d].Plan.Always })
}

// failFast cancels every waiting task on an unabsorbed failure, except the
// failed task's Always dependents (like GitHub Actions' always()). Each
// cancelled task is dropped so those Always dependents still count it.
func (s *wfState) failFast(failed string, push map[string]bool) {
	keep := map[string]bool{}
	for _, d := range s.next[failed] {
		keep[d] = s.Tasks[d].Plan.Always
	}
	var cancelled []string
	for _, name := range s.Order {
		if t := s.Tasks[name]; t.State == JobWaiting && !keep[name] {
			t.State, t.Finished = JobCancelled, s.At
			cancelled = append(cancelled, name)
		}
	}
	for _, name := range cancelled {
		s.drop(name, push)
	}
}

// reach returns every task reachable from name: ancestors when up, else
// descendants. name itself is not included.
func (s *wfState) reach(name string, up bool) map[string]bool {
	seen := map[string]bool{}
	stack := []string{name}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		edges := s.next[n]
		if up {
			edges = s.Tasks[n].Plan.Deps
		}
		for _, m := range edges {
			if !seen[m] {
				seen[m] = true
				stack = append(stack, m)
			}
		}
	}
	return seen
}

func (s *wfState) cancelWaiting() {
	for _, name := range s.Order {
		if t := s.Tasks[name]; t.State == JobWaiting {
			t.State, t.Finished = JobCancelled, s.At
		}
	}
}

// settle emits the pushes and, once the forward run is over, ends the
// workflow or starts compensation.
func (s *wfState) settle(push map[string]bool) []wfEffect {
	fx := s.emit(push, false)
	for _, name := range s.Order {
		switch s.Tasks[name].State {
		case JobWaiting, JobPending, JobProcessing:
			return fx
		}
	}
	// A failure before cancel keeps its compensation; cancel alone
	// compensates only when asked.
	undo := s.failed || s.cancelled && s.UndoOnCancel
	if undo {
		if undos := s.compensate(); len(undos) > 0 {
			s.State = WorkflowCompensating
			return append(fx, undos...)
		}
	}
	switch {
	case s.failed:
		return s.end(fx, WorkflowFailed)
	case s.cancelled:
		return s.end(fx, WorkflowCancelled)
	}
	return s.end(fx, WorkflowCompleted)
}

// compensate marks the undo of every completed task waiting on the undos of
// all its completed descendants that have one (transitive, so tasks without
// an undo are looked through: saga order). Returns the undos ready now.
func (s *wfState) compensate() []wfEffect {
	push := map[string]bool{}
	for _, name := range s.Order {
		t := s.Tasks[name]
		if t.State != JobCompleted || t.Plan.Undo == nil {
			continue
		}
		t.UndoState, t.UndoOpen = JobWaiting, 0
		for d := range s.reach(name, false) { // a count: map order is harmless
			if dt := s.Tasks[d]; dt.State == JobCompleted && dt.Plan.Undo != nil {
				t.UndoOpen++
			}
		}
		if t.UndoOpen == 0 {
			t.UndoState = JobPending
			push[name] = true
		}
	}
	return s.emit(push, true)
}

func (s *wfState) end(fx []wfEffect, state WorkflowState) []wfEffect {
	s.State = state
	return append(fx, wfEffect{End: state})
}

// emit turns a push set into effects in Order.
func (s *wfState) emit(push map[string]bool, undo bool) []wfEffect {
	if len(push) == 0 {
		return nil
	}
	fx := make([]wfEffect, 0, len(push))
	for _, name := range s.Order {
		if push[name] {
			fx = append(fx, wfEffect{Push: name, Undo: undo})
		}
	}
	return fx
}

func active(st JobState) bool {
	return st == JobPending || st == JobProcessing
}
