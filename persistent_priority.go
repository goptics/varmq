package varmq

type PersistentPriorityQueue[T any] interface {
	IExternalBaseQueue

	// Worker returns the bound worker.
	Worker() Worker
	// Add adds a new Job with the given priority to the queue
	// It returns true if the job was added successfully
	Add(data T, priority int, configs ...JobConfigFunc) bool
}

type persistentPriorityQueue[T any] struct {
	*priorityQueue[T]
}

func newPersistentPriorityQueue[T any](w *worker[T, iJob[T]], pq IPersistentPriorityQueue, configs ...QueueConfigFunc) PersistentPriorityQueue[T] {
	c := loadQueueConfigs(configs...)
	w.queues.Register(pq, c.Priority)

	return &persistentPriorityQueue[T]{
		priorityQueue: newPriorityQueue(w, pq, configs...),
	}
}

func (q *persistentPriorityQueue[T]) Add(data T, priority int, configs ...JobConfigFunc) bool {
	j := newJob(data, loadJobConfigs(q.w.configs(), configs...))
	val, err := j.Json()

	if err != nil {
		return false
	}

	if ok := q.internalQueue.Enqueue(val, priority); !ok {
		return false
	}

	q.w.Metrics().incSubmitted()
	q.w.notifyToPullNextJobs()

	return true
}

func (q *persistentPriorityQueue[T]) Purge() {
	q.priorityQueue.Purge()
}
