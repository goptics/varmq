package varmq

type PersistentPriorityQueue[T any] interface {
	TaskQueue[T] // a workflow task can target it
	IExternalBaseQueue

	// Worker returns the bound worker.
	Worker() Worker
	// Add adds a new Job with the given priority to the queue
	// It returns true if the job was added successfully
	Add(data T, priority int, configs ...JobConfigFunc) bool
}

type persistentPriorityQueue[T any] struct {
	*priorityQueue[T]
	poke func() // the source's ready hint; nil unless the storage pops in batches
}

func newPersistentPriorityQueue[T any, JobType iJob[T]](w *worker[T, JobType], pq IPriorityQueue, configs ...QueueConfigFunc) PersistentPriorityQueue[T] {
	c := loadQueueConfigs(configs...)
	poke := pokeOf(w.bind(pq, c.priority))

	return &persistentPriorityQueue[T]{priorityQueue: &priorityQueue[T]{
		externalBaseQueue: newExternalQueue(pq, w, c, nil), // never batches: nothing waits in a buffer
		internalQueue:     pq,
	}, poke: poke}
}

func (q *persistentPriorityQueue[T]) Add(data T, priority int, configs ...JobConfigFunc) bool {
	j := newJob(data, loadJobConfigs(q.w.configs(), configs...))
	val, err := j.Json()

	if err != nil {
		return false
	}

	if ok := enqueuePriority(q.internalQueue, val, priority, j.id); !ok {
		return false
	}

	if q.poke != nil {
		q.poke() // after the storage confirmed: a pop must not mark it empty before the item lands
	}
	q.w.notifyToPullNextJobs()

	return true
}

// WorkflowItem overrides the embedded in-memory queue's: the
// storage finishes and promotes tasks (workflow_target.go).
func (q *persistentPriorityQueue[T]) WorkflowItem(data T) (WorkflowItem, error) {
	return durableWorkflowItem(q.internalQueue, true, data)
}
