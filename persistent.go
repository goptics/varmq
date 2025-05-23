package varmq

// PersistentQueue is an interface that extends Queue to support persistent job operations
// where jobs can be recovered even after application restarts. All jobs must have unique IDs.
type PersistentQueue[T, R any] interface {
	Queue[T, R]
}

type persistentQueue[T, R any] struct {
	*queue[T, R]
}

// newPersistentQueue creates a new persistent queue with the given worker and internal queue
// The worker's queue is set to the provided persistent queue implementation
func newPersistentQueue[T, R any](w *worker[T, R], pq IPersistentQueue) PersistentQueue[T, R] {
	w.setQueue(pq)
	return &persistentQueue[T, R]{queue: &queue[T, R]{
		externalQueue: newExternalQueue(w),
		internalQueue: pq,
	}}
}

// Add adds a job with the given data to the persistent queue
// It requires a job ID to be provided in the job config for persistence
// It will panic if no job ID is provided
// Returns an EnqueuedJob that can be used to track the job's status and result
func (q *persistentQueue[T, R]) Add(data T, configs ...JobConfigFunc) (EnqueuedJob[R], bool) {
	jobConfig := withRequiredJobId(loadJobConfigs(q.configs, configs...))

	j := newJob[T, R](data, jobConfig)
	val, err := j.Json()

	if err != nil {
		return nil, false
	}

	if ok := q.internalQueue.Enqueue(val); !ok {
		j.close()
		return nil, false
	}

	j.SetInternalQueue(q.internalQueue)

	q.postEnqueue(j)

	return j, true
}

// AddAll adds multiple jobs to the persistent queue at once
// Each item must have a unique ID for persistence
// Returns an EnqueuedGroupJob that can be used to track all jobs' statuses and results
// Will panic if any job is missing an ID
func (q *persistentQueue[T, R]) AddAll(items []Item[T]) EnqueuedGroupJob[R] {
	l := len(items)
	groupJob := newGroupJob[T, R](l)

	for _, item := range items {
		jConfigs := withRequiredJobId(loadJobConfigs(q.configs, WithJobId(item.ID)))

		j := groupJob.newJob(item.Value, jConfigs)
		val, err := j.Json()

		if err != nil {
			j.close()
			continue
		}

		if ok := q.internalQueue.Enqueue(val); !ok {
			j.close()
			continue
		}

		j.SetInternalQueue(q.internalQueue)
		q.postEnqueue(j)
	}

	return groupJob
}

// Purge removes all jobs from the queue
func (q *persistentQueue[T, R]) Purge() {
	q.queue.Purge()

	// close all pending jobs
	q.worker.Cache.Range(func(_, value any) bool {
		if v, ok := value.(EnqueuedJob[R]); ok && v.Status() == "Queued" {
			v.close()
		}

		return true
	})
}

// Close stops the worker and closes the underlying queue
// Returns any error encountered while closing the queue
func (q *persistentQueue[T, R]) Close() error {
	defer q.Stop()
	return q.Queue.Close()
}
