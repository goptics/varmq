package varmq

// PersistentQueue is an interface that extends Queue to support persistent job operations
type PersistentQueue[T any] interface {
	IExternalQueue

	// Add adds a job with the given data to the persistent queue
	// It returns true if the job was added successfully
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
func (q *persistentQueue[T]) Add(data T, configs ...JobConfigFunc) bool {
	j := newJob(data, loadJobConfigs(q.w.configs(), configs...))
	val, err := j.Json()

	if err != nil {
		return false
	}

	if ok := q.internalQueue.Enqueue(val); !ok {
		j.Close()
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
