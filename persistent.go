package varmq

// PersistentQueue is an interface that extends Queue to support persistent job operations
// where jobs can be recovered even after application restarts. All jobs must have unique IDs.
type PersistentQueue[T, R any] interface {
	IExternalQueue[T, R]

	Add(data T, configs ...JobConfigFunc) bool
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
func (q *persistentQueue[T, R]) Add(data T, configs ...JobConfigFunc) bool {
	j := q.newJob(data, loadJobConfigs(q.configs, configs...))
	val, err := j.Json()

	if err != nil {
		return false
	}

	if ok := q.internalQueue.Enqueue(val); !ok {
		j.close()
		return false
	}

	q.notifyToPullNextJobs()

	return true
}

// Purge removes all jobs from the queue
func (q *persistentQueue[T, R]) Purge() {
	q.queue.Purge()
}

// Close stops the worker and closes the underlying queue
func (q *persistentQueue[T, R]) Close() error {
	defer q.Stop()
	return q.Queue.Close()
}
