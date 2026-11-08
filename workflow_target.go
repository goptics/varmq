package varmq

import (
	"errors"
	"fmt"
)

// TaskQueue is a queue handle a workflow task targets: every handle varmq
// returns (Queue, PriorityQueue, their Err and Result kinds, persistent and
// distributed ones). T is the job data type, so Add checks q and data
// together at compile time:
//
//	wf.Add("thumb", thumbQ, Thumb{Size: 64}) // thumbQ: Queue[Thumb]
type TaskQueue[T any] interface {
	// WorkflowItem is data as a job of this queue. Used by Workflow.Add and
	// Task.Undo; not for direct use.
	WorkflowItem(data T) (WorkflowItem, error)
}

// WorkflowItem is a job bound for a queue handle, made by
// TaskQueue.WorkflowItem. Used by Workflow.Add and Task.Undo; not for
// direct use.
type WorkflowItem struct {
	target taskTarget
	// build makes the item the store pushes: envelope bytes for durable
	// storage, the tagged job for in-memory storage. Called at Submit.
	build func(jobID string, tag taskTag) (any, error)
}

// taskTarget is where a task's job goes.
type taskTarget struct {
	queueID  string
	queue    any // TaskPlan.Queue: memTarget or the durable storage
	scope    string
	priority bool
}

// memTarget is an in-memory queue in TaskPlan.Queue: the in-memory store
// pushes promoted jobs through it.
type memTarget struct {
	queueID string
	push    func(item any, prio int) bool
}

// memTaskJob is what memTarget.push needs of a job: every job kind has it.
type memTaskJob interface {
	ID() string
	markQueued()
}

// memoryScope is the Atomic Scope of every in-memory queue of this process.
func memoryScope() string { return "memory:" + InstanceID }

// memWorkflowItem is a WorkflowItem of an in-memory handle. store puts item
// into the storage once per id (FIFO storage ignores prio); mk makes the
// job of the kind the handle's worker runs, and its embedded job.
func memWorkflowItem[T any](eq *externalBaseQueue, priority bool, store func(item any, prio int, id string) bool,
	mk func(jobID string) (any, *job[T])) (WorkflowItem, error) {
	m := eq.totals
	if m == nil { // storage that counts or acks on its own: not plain in-memory
		return WorkflowItem{}, fmt.Errorf("varmq: queue storage %T can't take workflow tasks", eq.q)
	}

	w := eq.w
	push := func(item any, prio int) bool {
		j, ok := item.(memTaskJob)
		if !ok {
			return false
		}

		m.added(1)
		if !store(item, prio, j.ID()) {
			m.added(-1)
			return false
		}

		j.markQueued()
		w.notifyToPullNextJobs()

		return true
	}

	// Its own ack, counted by m and tagged (tag.store nil: the store binds
	// it). Not countedBy: m.ref is shared and has no tag.
	build := func(jobID string, tag taskTag) (any, error) {
		item, j := mk(jobID)
		j.ackRef.Store(&jobAck{totals: m, ext: &ackExt{task: &tag}})
		return item, nil
	}

	return WorkflowItem{
		target: taskTarget{queueID: m.id, queue: memTarget{queueID: m.id, push: push}, scope: memoryScope(), priority: priority},
		build:  build,
	}, nil
}

// onceForcer and priorityOnceForcer are storage that can store past its
// capacity (the internal queues): promotion ignores capacity. Other storage
// gets a plain EnqueueOnce, which may refuse.
type onceForcer interface {
	EnqueueOnceForce(item any, id string) bool
}

type priorityOnceForcer interface {
	EnqueueOnceForce(item any, priority int, id string) bool
}

func memFIFOItem[T any](eq *externalBaseQueue, q IQueue, mk func(string) (any, *job[T])) (WorkflowItem, error) {
	store := func(item any, _ int, id string) bool { return enqueue(q, item, id) }
	if f, ok := q.(onceForcer); ok {
		store = func(item any, _ int, id string) bool { return f.EnqueueOnceForce(item, id) }
	}

	return memWorkflowItem(eq, false, store, mk)
}

func memPriorityItem[T any](eq *externalBaseQueue, q IPriorityQueue, mk func(string) (any, *job[T])) (WorkflowItem, error) {
	store := func(item any, prio int, id string) bool { return enqueuePriority(q, item, prio, id) }
	if f, ok := q.(priorityOnceForcer); ok {
		store = func(item any, prio int, id string) bool { return f.EnqueueOnceForce(item, prio, id) }
	}

	return memWorkflowItem(eq, true, store, mk)
}

// taskJob, errTaskJob and resultTaskJob make data's job for a Worker,
// ErrWorker and ResultWorker queue.
func taskJob[T any](data T) func(string) (any, *job[T]) {
	return func(id string) (any, *job[T]) {
		j := newJob(data, jobConfigs{Id: id})
		return j, j
	}
}

func errTaskJob[T any](data T) func(string) (any, *job[T]) {
	return func(id string) (any, *job[T]) {
		j := newErrorJob(data, jobConfigs{Id: id})
		return j, &j.job
	}
}

func resultTaskJob[T, R any](data T) func(string) (any, *job[T]) {
	return func(id string) (any, *job[T]) {
		j := newResultJob[T, R](data, jobConfigs{Id: id})
		return j, &j.job
	}
}

// durableWorkflowItem is a WorkflowItem of persistent or distributed
// storage, which must finish workflow tasks itself.
func durableWorkflowItem[T any](storage any, priority bool, data T) (WorkflowItem, error) {
	qi, inspectable := storage.(QueueInspector)
	wa, ok := storage.(WorkflowAcknowledgeable)
	if !ok || !inspectable {
		name := fmt.Sprintf("%T", storage)
		if inspectable {
			name = qi.QueueID()
		}

		return WorkflowItem{}, fmt.Errorf("%w: queue %s can't take workflow tasks", ErrScopeMismatch, name)
	}
	if wa.WorkflowStore() == nil { // its tasks would skip the pre-run check
		return WorkflowItem{}, fmt.Errorf("%w: queue %s: storage has no workflow store", ErrScopeMismatch, qi.QueueID())
	}

	build := func(jobID string, tag taskTag) (any, error) {
		j := newJob(data, jobConfigs{Id: jobID})
		j.setTask(tag)
		return j.Json()
	}

	return WorkflowItem{
		target: taskTarget{queueID: qi.QueueID(), queue: storage, scope: wa.ScopeID(), priority: priority},
		build:  build,
	}, nil
}

var errNilTaskQueue = errors.New("varmq: nil queue")

// itemOf is data as a job of q.
func itemOf[T any](q TaskQueue[T], data T) (WorkflowItem, error) {
	if q == nil {
		return WorkflowItem{}, errNilTaskQueue
	}

	return q.WorkflowItem(data)
}
