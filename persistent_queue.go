package gocq

var errJobIdRequired = "job id is required for persistent queue"

type PersistentQueue[T, R any] interface {
	Queue[T, R]
}

type persistentQueue[T, R any] struct {
	*queue[T, R]
}

func newPersistentQueue[T, R any](w *worker[T, R], pq IQueue) PersistentQueue[T, R] {
	w.setQueue(pq)
	return &persistentQueue[T, R]{queue: &queue[T, R]{
		externalQueue: newExternalQueue(w),
		internalQueue: pq,
	}}
}

func (q *persistentQueue[T, R]) Add(data T, configs ...JobConfigFunc) EnqueuedJob[R] {
	jobConfig := loadJobConfigs(q.configs, configs...)

	if jobConfig.Id == "" {
		panic(errJobIdRequired)
	}

	j := newJob[T, R](data, jobConfig)
	val, _ := j.Json()

	q.internalQueue.Enqueue(val)
	q.postEnqueue(j)

	return j
}

func (q *persistentQueue[T, R]) AddAll(items []Item[T]) EnqueuedGroupJob[R] {
	groupJob := newGroupJob[T, R](uint32(len(items)))

	for _, item := range items {
		jConfigs := loadJobConfigs(q.configs, WithJobId(item.ID))
		if jConfigs.Id == "" {
			panic(errJobIdRequired)
		}

		j := groupJob.NewJob(item.Value, jConfigs)
		val, _ := j.Json()

		ok := q.internalQueue.Enqueue(val)

		if !ok {
			groupJob.wg.Done()
			continue
		}
		q.postEnqueue(j)
	}

	return groupJob
}

func (q *persistentQueue[T, R]) Purge() {
	q.queue.Purge()
}

func (q *persistentQueue[T, R]) Close() error {
	defer q.Stop()
	return q.Queue.Close()
}
