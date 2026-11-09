package contract

import (
	"context"
	"errors"
	"sync"
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

// UndoPlan is a task's compensation job. A priority queue's undo is pushed
// at priority 0.
type UndoPlan struct {
	JobID, QueueID string
	Queue          any
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
	ID           string        `json:"id"`
	State        WorkflowState `json:"state"`
	CreatedAt    int64         `json:"created_at"`
	FinishedAt   int64         `json:"finished_at,omitempty"`
	UndoOnCancel bool          `json:"undo_on_cancel"` // the plan's, or the override a Cancel gave
	Tasks        []TaskRecord  `json:"tasks"`
}

// Scoped names the storage scope. Two durable objects with the same ScopeID
// share state, so a workflow may span their queues. In-memory stores all
// share the process's scope ID but not state: a workflow's queues use the
// store it was submitted to.
type Scoped interface {
	ScopeID() string
}

// WorkflowStore is the workflow state a storage keeps.
type WorkflowStore interface {
	Scoped
	// Submit stores the plan and pushes its root tasks.
	Submit(ctx context.Context, w WorkflowPlan) error
	// Cancel cancels the workflow. undo nil: compensation runs as the
	// workflow was submitted (WorkflowPlan.UndoOnCancel); non-nil overrides.
	Cancel(ctx context.Context, id string, undo *bool) error
	// Runnable reports whether the task (undo: its undo job) may run now:
	// the pre-run check.
	Runnable(ctx context.Context, workflowID, task string, undo bool) (bool, error)
	// DepResult returns the stored result of the task's dependency.
	DepResult(ctx context.Context, workflowID, task string) ([]byte, error)
	Record(ctx context.Context, id string) (WorkflowRecord, error)
	// FinishTask settles the task's job (ackID on the queue queueID), records
	// the outcome and pushes what it releases, in one atomic step (or, for
	// storage that can't, one that redelivery and Runnable make safe). It
	// returns the IDs of the queues it pushed into, so varmq pokes its local
	// consumers of them. A re-finish of a task that already ended (a lost
	// reply, a redelivery) changes nothing and returns every push-target
	// queue ID (all task and undo queues of the workflow), so a wake lost with
	// the first reply is not lost for good. A workflow that is gone (expired)
	// settles the item and returns ok with no IDs: false would redeliver it
	// forever.
	FinishTask(queueID, ackID string, e TaskEnd) (pushed []string, ok bool)
}

// WorkflowInspector lists workflows.
type WorkflowInspector interface {
	Scoped
	Workflows(ctx context.Context, state WorkflowState, offset, limit int) ([]WorkflowRecord, error)
}

// WorkflowKey is what a workflow listing sorts on: CreatedAt (Unix ms)
// newest first, then ID.
type WorkflowKey struct {
	ID        string
	CreatedAt int64
}

// WorkflowKeyLister is optional for a WorkflowInspector: Workflows without
// the records (same filter, order and paging), so a reader that merges
// several stores reads (Record) only the workflows of its final page.
type WorkflowKeyLister interface {
	WorkflowKeys(ctx context.Context, state WorkflowState, offset, limit int) ([]WorkflowKey, error)
}

var (
	ErrWorkflowExists   = errors.New("varmq: workflow already exists")
	ErrWorkflowNotFound = errors.New("varmq: workflow not found")
	ErrNoResult         = errors.New("varmq: task has no result")
	ErrDepFailed        = errors.New("varmq: dependency failed")
	ErrResultTooLarge   = errors.New("varmq: task result too large")
	ErrScopeMismatch    = errors.New("varmq: queues are in different storage scopes")
	ErrScopeRegistered  = errors.New("varmq: a workflow store is already registered for this scope")
)

// Durable workflow stores of this process, by ScopeID. A queue names only its
// scope (Scoped); varmq finds the store of a task's queue here. Storage
// registers its store when it is made (redisq.NewWorkflowStore, ...), so a
// process that only runs workers needs that one call. In-memory stores share
// one scope ID but not state, so varmq keeps them itself; they don't register.
// ponytail: one process-wide map; nothing scopes it per test but Unregister.
var (
	storesMu sync.RWMutex
	stores   = map[string]WorkflowStore{}
)

// RegisterWorkflowStore makes s the store of its scope. Registering s again
// is a no-op; another store for the same scope is ErrScopeRegistered.
func RegisterWorkflowStore(s WorkflowStore) error {
	storesMu.Lock()
	defer storesMu.Unlock()
	scope := s.ScopeID()
	if cur, ok := stores[scope]; ok && cur != s {
		return ErrScopeRegistered
	}
	stores[scope] = s
	return nil
}

// UnregisterWorkflowStore removes s, if it is the registered store of its
// scope (a closed store).
func UnregisterWorkflowStore(s WorkflowStore) {
	storesMu.Lock()
	defer storesMu.Unlock()
	if cur, ok := stores[s.ScopeID()]; ok && cur == s {
		delete(stores, s.ScopeID())
	}
}

// LookupWorkflowStore returns the store registered for scope.
func LookupWorkflowStore(scope string) (WorkflowStore, bool) {
	storesMu.RLock()
	defer storesMu.RUnlock()
	s, ok := stores[scope]
	return s, ok
}

// WorkflowStores returns every registered store, in no set order.
func WorkflowStores() []WorkflowStore {
	storesMu.RLock()
	defer storesMu.RUnlock()
	out := make([]WorkflowStore, 0, len(stores))
	for _, s := range stores {
		out = append(out, s)
	}
	return out
}
