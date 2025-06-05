package varmq

// PersistentQueue is an interface that extends Queue to support persistent job operations
type PersistentQueue[T any] interface {
	IExternalBaseQueue

	// Worker returns the bound worker.
	Worker() Worker

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
	return &persistentQueue[T]{queue: &queue[T]{
		externalBaseQueue: newExternalQueue(pq, w),
		internalQueue:     pq,
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
