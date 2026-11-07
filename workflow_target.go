package varmq

import "fmt"

// TaskQueue is a queue handle a workflow task targets: any handle varmq
// returns (Queue, PriorityQueue, ErrQueue, ResultQueue, persistent and
// distributed ones). T is the job data type.
// ponytail: T only drives inference. Handle interfaces don't carry the
// unexported taskQueue methods (that would change them for users), so a
// handle of another data type is caught by plan, not by the compiler.
type TaskQueue[T any] interface {
	IExternalBaseQueue
}

// taskQueue is what every handle type implements for workflows.
type taskQueue[T any] interface {
	taskTarget() taskTarget
	// taskItem is the item the store pushes: envelope bytes for durable
	// storage, the tagged job for in-memory storage.
	taskItem(data T, jobID string, tag taskTag) (any, error)
}

// taskTarget is where a task's job goes. err is set when the handle can't
// take workflow tasks.
type taskTarget struct {
	queueID  string
	queue    any // TaskPlan.Queue: memTarget or the durable storage
	scope    string
	priority bool
	err      error
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

// memTaskTarget targets an in-memory handle. store puts item into the
// storage once per id (prio is ignored by FIFO storage).
// ponytail: storage still applies its own capacity; promotion can't bypass
// it without a storage method.
func memTaskTarget(eq *externalBaseQueue, priority bool, store func(item any, prio int, id string) bool) taskTarget {
	m := eq.totals
	if m == nil { // storage that counts or acks on its own: not plain in-memory
		return taskTarget{err: fmt.Errorf("varmq: queue storage %T can't take workflow tasks", eq.q)}
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

	return taskTarget{queueID: m.id, queue: memTarget{queueID: m.id, push: push}, scope: memoryScope(), priority: priority}
}

func memFIFOTarget(eq *externalBaseQueue, q IQueue) taskTarget {
	return memTaskTarget(eq, false, func(item any, _ int, id string) bool { return enqueue(q, item, id) })
}

func memPriorityTarget(eq *externalBaseQueue, q IPriorityQueue) taskTarget {
	return memTaskTarget(eq, true, func(item any, prio int, id string) bool {
		return enqueuePriority(q, item, prio, id)
	})
}

// tagMemJob gives an in-memory task job its own ack: counted by m, tagged
// (tag.store nil: the store binds it). Not countedBy: m.ref is shared and
// has no tag.
func tagMemJob[T any](j *job[T], m *memQueue, tag taskTag) {
	j.ackRef.Store(&jobAck{totals: m, ext: &ackExt{task: &tag}})
}

// durableTaskTarget targets persistent or distributed storage, which must
// finish workflow tasks itself.
func durableTaskTarget(storage any, priority bool) taskTarget {
	qi, inspectable := storage.(QueueInspector)
	wa, ok := storage.(WorkflowAcknowledgeable)
	if !ok || !inspectable {
		name := fmt.Sprintf("%T", storage)
		if inspectable {
			name = qi.QueueID()
		}

		return taskTarget{err: fmt.Errorf("%w: queue %s can't take workflow tasks", ErrScopeMismatch, name)}
	}

	return taskTarget{queueID: qi.QueueID(), queue: storage, scope: wa.ScopeID(), priority: priority}
}

// durableTaskItem is the job envelope of a task, tagged with its workflow.
func durableTaskItem[T any](data T, jobID string, tag taskTag) (any, error) {
	j := newJob(data, jobConfigs{Id: jobID})
	j.setTask(tag.wf, tag.task)

	return j.Json()
}
