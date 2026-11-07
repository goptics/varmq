package contract

import (
	"context"
	"errors"
)

// Outcome is how a workflow task ended.
type Outcome string

const (
	OutcomeCompleted Outcome = "completed"
	OutcomeFailed    Outcome = "failed"
	OutcomeCancelled Outcome = "cancelled"
)

// WorkflowState is where a workflow is in its life.
type WorkflowState string

const (
	WorkflowRunning            WorkflowState = "running"
	WorkflowCompleted          WorkflowState = "completed"
	WorkflowFailed             WorkflowState = "failed"
	WorkflowCompensating       WorkflowState = "compensating"
	WorkflowCompensated        WorkflowState = "compensated"
	WorkflowCompensationFailed WorkflowState = "compensation_failed"
	WorkflowCancelled          WorkflowState = "cancelled"
)

// WorkflowPlan is a validated DAG handed to WorkflowStore.Submit.
type WorkflowPlan struct {
	ID           string
	FailFast     bool
	UndoOnCancel bool
	Tasks        []TaskPlan
	Hash         string // hex SHA-256 of the plan shape
}

// TaskPlan is one node. Item and Queue are any: bytes and the storage object
// for durable queues, the *job and in-memory storage for in-memory ones. Each
// store asserts its own types.
type TaskPlan struct {
	Name, JobID, QueueID string
	Queue                any
	Priority             int
	HasPriority          bool
	Deps                 []string
	Always               bool
	Item                 any
	Undo                 *UndoPlan
}

// UndoPlan is a task's compensation job.
type UndoPlan struct {
	JobID, QueueID string
	Queue          any
	Priority       int
	HasPriority    bool
	Item           any
}

// TaskEnd reports how a task (or its undo) ended.
type TaskEnd struct {
	Workflow, Task string
	Outcome        Outcome
	Reason         string
	Result         []byte
	Undo           bool
}

// TaskRecord is a task as storage holds it.
type TaskRecord struct {
	Name       string      `json:"name"`
	State      JobState    `json:"state"`
	Deps       []string    `json:"deps,omitempty"`
	QueueID    string      `json:"queue_id"`
	JobID      string      `json:"job_id"`
	Always     bool        `json:"always,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	ResultSize int         `json:"result_size,omitempty"`
	Undo       *TaskRecord `json:"undo,omitempty"`
}

// WorkflowRecord is a workflow as storage holds it. Times are Unix ms.
type WorkflowRecord struct {
	ID         string        `json:"id"`
	State      WorkflowState `json:"state"`
	CreatedAt  int64         `json:"created_at"`
	FinishedAt int64         `json:"finished_at,omitempty"`
	Tasks      []TaskRecord  `json:"tasks"`
}

// Scoped names the storage scope. Two objects with the same ScopeID share
// state, so a workflow may span their queues.
type Scoped interface {
	ScopeID() string
}

// WorkflowStore is the workflow state a storage keeps.
type WorkflowStore interface {
	Scoped
	// Submit stores the plan and pushes its root tasks.
	Submit(ctx context.Context, w WorkflowPlan) error
	// Cancel cancels the workflow. undo also runs compensation.
	Cancel(ctx context.Context, id string, undo bool) error
	// Runnable reports whether the task (undo: its undo job) may run now:
	// the pre-run check.
	Runnable(ctx context.Context, workflowID, task string, undo bool) (bool, error)
	// DepResult returns the stored result of the task's dependency.
	DepResult(ctx context.Context, workflowID, task string) ([]byte, error)
	Record(ctx context.Context, id string) (WorkflowRecord, error)
}

// WorkflowAcknowledgeable is a queue storage that can finish workflow tasks.
type WorkflowAcknowledgeable interface {
	Scoped
	WorkflowStore() WorkflowStore
	// FinishTask records the outcome and returns the ids of jobs it pushed.
	FinishTask(ackID string, e TaskEnd) (pushed []string, ok bool)
}

// WorkflowInspector lists workflows.
type WorkflowInspector interface {
	Scoped
	Workflows(ctx context.Context, state WorkflowState, offset, limit int) ([]WorkflowRecord, error)
}

var (
	ErrWorkflowExists   = errors.New("varmq: workflow already exists")
	ErrWorkflowNotFound = errors.New("varmq: workflow not found")
	ErrNoResult         = errors.New("varmq: task has no result")
	ErrDepFailed        = errors.New("varmq: dependency failed")
	ErrResultTooLarge   = errors.New("varmq: task result too large")
	ErrScopeMismatch    = errors.New("varmq: queues are in different storage scopes")
	ErrTaskJobID        = errors.New("varmq: task job id conflicts")
)
