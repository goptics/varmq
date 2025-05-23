package varmq

type PersistentPriorityQueue[T, R any] interface {
	PriorityQueue[T, R]
}

type persistentPriorityQueue[T, R any] struct {
	*priorityQueue[T, R]
}

func newPersistentPriorityQueue[T, R any](worker *worker[T, R], pq IPersistentPriorityQueue) PersistentPriorityQueue[T, R] {
	worker.setQueue(pq)
	return &persistentPriorityQueue[T, R]{
		priorityQueue: newPriorityQueue(worker, pq),
	}
}

func (q *persistentPriorityQueue[T, R]) Add(data T, priority int, configs ...JobConfigFunc) (EnqueuedJob[R], bool) {
	jobConfig := withRequiredJobId(loadJobConfigs(q.configs, configs...))

	j := newJob[T, R](data, jobConfig)
	val, err := j.Json()
	if err != nil {
		return nil, false
	}
	j.SetInternalQueue(q.internalQueue)

	if ok := q.internalQueue.Enqueue(val, priority); !ok {
		j.close()
		return nil, false
	}

	q.postEnqueue(j)

	return j, true
}

func (q *persistentPriorityQueue[T, R]) AddAll(items []Item[T]) EnqueuedGroupJob[R] {
	groupJob := newGroupJob[T, R](len(items))

	for _, item := range items {
		jConfigs := withRequiredJobId(loadJobConfigs(q.configs, WithJobId(item.ID)))

		j := groupJob.newJob(item.Value, jConfigs)
		val, err := j.Json()
		if err != nil {
			j.close()
			continue
		}
		j.SetInternalQueue(q.internalQueue)

		if ok := q.internalQueue.Enqueue(val, item.Priority); !ok {
			j.close()
			continue
		}

		q.postEnqueue(j)
	}

	return groupJob
}

func (q *persistentPriorityQueue[T, R]) Purge() {
	q.Queue.Purge()
}

func (q *persistentPriorityQueue[T, R]) Close() error {
	defer q.Stop()
	return q.Queue.Close()
}
