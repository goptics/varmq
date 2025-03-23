package gocq

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
	j := newJob[T, R](data, loadJobConfigs(q.configs, configs...))
	val, _ := j.Json()

	q.queue.Enqueue(val)
	q.postEnqueue(j)

	return j
}

func (q *concurrentPersistentQueue[T, R]) AddAll(items []Item[T]) EnqueuedGroupJob[R] {
	groupJob := newGroupJob[T, R](uint32(len(items)))

	q.sync.wg.Add(len(items))
	for _, item := range items {
		j := groupJob.NewJob(item.Value, item.ID)
		val, _ := j.Json()

		q.queue.Enqueue(val)
		q.postEnqueue(j)
	}

	return groupJob
}
