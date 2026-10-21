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
	poke func() // the source's ready hint; nil unless the storage pops in batches
}

// newPersistentQueue creates a new persistent queue with the given worker and internal queue
// The worker's queue is set to the provided persistent queue implementation
func newPersistentQueue[T any](w *worker[T, iJob[T]], pq IPersistentQueue, configs ...QueueConfigFunc) PersistentQueue[T] {
	c := loadQueueConfigs(configs...)
	poke := pokeOf(w.bind(pq, c.priority))

	return &persistentQueue[T]{queue: &queue[T]{
		externalBaseQueue: newExternalQueue(pq, w, c, nil), // never batches: nothing waits in a buffer
		internalQueue:     pq,
	}, poke: poke}
}

// Add adds a job with the given data to the persistent queue
func (q *persistentQueue[T]) Add(data T, configs ...JobConfigFunc) bool {
	j := newJob(data, loadJobConfigs(q.w.configs(), configs...))
	val, err := j.Json()

	if err != nil {
		return false
	}

	if ok := enqueue(q.internalQueue, val, j.id); !ok {
		j.Close()
		return false
	}

	q.w.Metrics().incSubmitted()
	if q.poke != nil {
		q.poke() // after the storage confirmed: a pop must not mark it empty before the item lands
	}
	q.w.notifyToPullNextJobs()

	return true
}
