package varmq

type priorityQueue[T, R any] struct {
	*externalQueue[T, R]
	internalQueue IPriorityQueue
}

type PriorityQueue[T, R any] interface {
	IExternalQueue[T, R]
	// Add adds a new Job with the given priority to the queue and returns a channel to receive the result.
	// Time complexity: O(log n)
	Add(data T, priority int, configs ...JobConfigFunc) (EnqueuedJob[R], bool)
	// AddAll adds multiple Jobs with the given priority to the queue and returns a channel to receive all responses.
	// Time complexity: O(n log n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedGroupJob[R]
}

// NewPriorityQueue creates a new priorityQueue with the specified concurrency and worker function.
func newPriorityQueue[T, R any](worker *worker[T, R], pq IPriorityQueue) *priorityQueue[T, R] {
	worker.setQueue(pq)

	return &priorityQueue[T, R]{
		externalQueue: newExternalQueue(worker),
		internalQueue: pq,
	}
}

func (q *priorityQueue[T, R]) Add(data T, priority int, configs ...JobConfigFunc) (EnqueuedJob[R], bool) {
	j := newJob[T, R](data, loadJobConfigs(q.configs, configs...))

	if ok := q.internalQueue.Enqueue(j, priority); !ok {
		j.close()
		return nil, false
	}

	q.postEnqueue(j)

	return j, true
}

func (q *priorityQueue[T, R]) AddAll(items []Item[T]) EnqueuedGroupJob[R] {
	l := len(items)
	groupJob := newGroupJob[T, R](l)

	for _, item := range items {
		j := groupJob.newJob(item.Value, loadJobConfigs(q.configs, WithJobId(item.ID)))

		if ok := q.internalQueue.Enqueue(j, item.Priority); !ok {
			j.close()
			continue
		}

		q.postEnqueue(j)
	}

	return groupJob
}
