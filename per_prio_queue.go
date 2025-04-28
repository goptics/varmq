package gocmq

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

func (q *persistentPriorityQueue[T, R]) Add(data T, priority int, configs ...JobConfigFunc) EnqueuedJob[R] {
	jobConfig := withRequiredJobId(loadJobConfigs(q.configs, configs...))

	j := newJob[T, R](data, jobConfig)
	val, _ := j.Json()

	q.internalQueue.Enqueue(val, priority)
	q.postEnqueue(j)

	return j
}

func (q *persistentPriorityQueue[T, R]) AddAll(items []PQItem[T]) EnqueuedGroupJob[R] {
	groupJob := newGroupJob[T, R](uint32(len(items)))

	for _, item := range items {
		jConfigs := withRequiredJobId(loadJobConfigs(q.configs, WithJobId(item.ID)))

		j := groupJob.NewJob(item.Value, jConfigs)
		val, _ := j.Json()

		ok := q.internalQueue.Enqueue(val, item.Priority)

		if !ok {
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
