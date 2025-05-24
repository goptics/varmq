package varmq

// PersistentQueue is an interface that extends Queue to support persistent job operations
// where jobs can be recovered even after application restarts. All jobs must have unique IDs.
type PersistentQueue[T any] interface {
	IExternalQueue

	Add(data T, configs ...JobConfigFunc) bool
}

type persistentQueue[T any] struct {
	*queue[T]
}

// newPersistentQueue creates a new persistent queue with the given worker and internal queue
// The worker's queue is set to the provided persistent queue implementation
func newPersistentQueue[T any](w *worker[T, iJob[T]], pq IPersistentQueue) PersistentQueue[T] {
	w.setQueue(pq)
	return &persistentQueue[T]{queue: &queue[T]{
		externalQueue: newExternalQueue(w),
		internalQueue: pq,
	}}
}

// Add adds a job with the given data to the persistent queue
// It requires a job ID to be provided in the job config for persistence
func (q *persistentQueue[T]) Add(data T, configs ...JobConfigFunc) bool {
	j := newJob[T](data, loadJobConfigs(q.w.configs(), configs...))
	val, err := j.Json()

	if err != nil {
		return false
	}

	if ok := q.internalQueue.Enqueue(val); !ok {
		j.close()
		return false
	}

	q.w.notifyToPullNextJobs()

	return true
}

// Purge removes all jobs from the queue
func (q *persistentQueue[T]) Purge() {
	q.queue.Purge()
}

// Close stops the worker and closes the underlying queue
func (q *persistentQueue[T]) Close() error {
	defer q.w.Stop()
	return q.internalQueue.Close()
}
