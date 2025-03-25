package gocq

var errJobIdRequired = "job id is required for persistent queue"

type ConcurrentPersistentQueue[T, R any] interface {
	ICQueue[T, R]
	// Add adds a new Job to the queue and returns a channel to receive the result.
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) EnqueuedJob[R]
	// AddAll adds multiple Jobs to the queue and returns a channel to receive all responses.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedGroupJob[R]
}

type concurrentPersistentQueue[T, R any] struct {
	*concurrentQueue[T, R]
}

func newPersistentQueue[T, R any](w *worker[T, R], pq IQueue) ConcurrentPersistentQueue[T, R] {
	w.setQueue(pq)
	return &concurrentPersistentQueue[T, R]{concurrentQueue: &concurrentQueue[T, R]{
		worker: w,
		queue:  pq,
	}}
}

func (q *concurrentPersistentQueue[T, R]) Add(data T, configs ...JobConfigFunc) EnqueuedJob[R] {
	jobConfig := loadJobConfigs(q.configs, configs...)

	if jobConfig.Id == "" {
		panic(errJobIdRequired)
	}

	j := newJob[T, R](data, jobConfig)
	val, _ := j.Json()

	q.queue.Enqueue(val)
	q.worker.sync.wg.Add(1)
	q.postEnqueue(j)

	return j
}

func (q *concurrentPersistentQueue[T, R]) AddAll(items []Item[T]) EnqueuedGroupJob[R] {
	groupJob := newGroupJob[T, R](uint32(len(items)))

	for _, item := range items {
		jConfigs := loadJobConfigs(q.configs, WithJobId(item.ID))
		if jConfigs.Id == "" {
			panic(errJobIdRequired)
		}

		j := groupJob.NewJob(item.Value, jConfigs)
		val, _ := j.Json()

		ok := q.queue.Enqueue(val)

		if !ok {
			continue
		}
		q.worker.sync.wg.Add(1)
		q.postEnqueue(j)
	}

	return groupJob
}

func (q *concurrentPersistentQueue[T, R]) Purge() {
	q.PauseAndWait()
	q.worker.sync.wg.Add(-q.Queue.Len())
	q.queue.Purge()
}

func (q *concurrentPersistentQueue[T, R]) Close() error {
	defer q.Stop()
	return q.Queue.Close()
}
