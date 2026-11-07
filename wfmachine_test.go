package varmq

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			s := newWFState(tc.plan)
			assert.Equal(t, WorkflowRunning, s.State)
			assert.Equal(t, tc.roots, fxStrings(s.roots()))
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
	s := newWFState(mplan(false,
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
	s := newWFState(mplan(false, mtask{name: "A"}))
	fx, err := s.finish(TaskEnd{Workflow: "wf", Task: "nope", Outcome: OutcomeCompleted})
	assert.Error(t, err)
	assert.Empty(t, fx)

	fx, err = s.finish(TaskEnd{Workflow: "wf", Task: "A", Outcome: "lost"})
	assert.Error(t, err)
	assert.Empty(t, fx)
	assert.Equal(t, JobPending, s.Tasks["A"].State)
}

func TestMachineKeepsReasonAndResult(t *testing.T) {
	s := newWFState(mplan(false, mtask{name: "A"}, mtask{name: "B"}))
	_, err := s.finish(TaskEnd{Task: "A", Outcome: OutcomeCompleted, Result: []byte(`1`)})
	require.NoError(t, err)
	_, err = s.finish(TaskEnd{Task: "B", Outcome: OutcomeFailed, Reason: "boom"})
	require.NoError(t, err)
	assert.Equal(t, []byte(`1`), s.Tasks["A"].Result)
	assert.Equal(t, "boom", s.Tasks["B"].Reason)
}

func TestMachineProcessingFinishes(t *testing.T) {
	// storage marks a popped task processing; finish still applies.
	s := newWFState(mplan(false, mtask{name: "A"}))
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
	s := newWFState(mplan(false,
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
		s := newWFState(plan)
		all := fxStrings(s.roots())
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
	s := newWFState(mplan(true,
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

	s = newWFState(mplan(false, mtask{name: "A"}, mtask{name: "X"}))
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
		s := newWFState(p)
		assert.Equal(t, tc.plan, s.UndoOnCancel)
		mustFinish(t, s, TaskEnd{Task: "A", Outcome: OutcomeCompleted})
		assert.Empty(t, s.cancel(tc.cancel), "B is queued")
		assert.Equal(t, tc.cancel, s.UndoOnCancel, "the cancel's value is kept")
		assert.Equal(t, tc.want, mustFinish(t, s, TaskEnd{Task: "B", Outcome: OutcomeCancelled}), "%+v", tc)
	}
}
