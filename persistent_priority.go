package varmq

type PersistentPriorityQueue[T any] interface {
	IExternalQueue
	// Add adds a new Job with the given priority to the queue and returns a channel to receive the result.
	// Time complexity: O(log n)
	Add(data T, priority int, configs ...JobConfigFunc) bool
}

type persistentPriorityQueue[T any] struct {
	*priorityQueue[T]
}

func newPersistentPriorityQueue[T any](worker *worker[T, iJob[T]], pq IPersistentPriorityQueue) PersistentPriorityQueue[T] {
	worker.setQueue(pq)
	return &persistentPriorityQueue[T]{
		priorityQueue: newPriorityQueue(worker, pq),
	}
}

func (q *persistentPriorityQueue[T]) Add(data T, priority int, configs ...JobConfigFunc) bool {
	j := newJob[T](data, loadJobConfigs(q.w.configs(), configs...))
	val, err := j.Json()

	if err != nil {
		return false
	}

	if ok := q.internalQueue.Enqueue(val, priority); !ok {
		return false
	}

	q.w.notifyToPullNextJobs()

	return true
}

func (q *persistentPriorityQueue[T]) Close() error {
	defer q.w.Stop()
	return q.internalQueue.Close()
}
