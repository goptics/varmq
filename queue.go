package varmq

// queue is the base implementation of the Queue interface
// It contains an externalQueue for worker management and an internalQueue for job storage
type queue[T, R any] struct {
	*externalQueue[T, R]
	internalQueue IQueue
}

type Queue[T, R any] interface {
	IExternalQueue[T, R]
	// Add adds a new Job to the queue and returns a EnqueuedJob to handle the job.
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) (EnqueuedJob[R], bool)
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedGroupJob to handle the job.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedGroupJob[R]
}

// Item represents a data item to be processed by a worker
// It combines a unique identifier with a value of any generic type
type Item[T any] struct {
	ID       string
	Value    T
	Priority int
}

// newQueue creates a new queue with the given worker and internal queue implementation
// It sets the worker's queue to the provided queue and creates a new external queue for job management
func newQueue[T, R any](worker *worker[T, R], q IQueue) *queue[T, R] {
	worker.setQueue(q)

	return &queue[T, R]{
		externalQueue: newExternalQueue(worker),
		internalQueue: q,
	}
}

func (q *queue[T, R]) Add(data T, configs ...JobConfigFunc) (EnqueuedJob[R], bool) {
	j := q.newJob(data, loadJobConfigs(q.configs, configs...))

	if ok := q.internalQueue.Enqueue(j); !ok {
		j.close()
		return nil, false
	}

	q.postEnqueue(j)

	return j, true
}

func (q *queue[T, R]) AddAll(items []Item[T]) EnqueuedGroupJob[R] {
	groupJob := q.newGroupJob(len(items))

	for _, item := range items {
		j := groupJob.newJob(item.Value, loadJobConfigs(q.configs, WithJobId(item.ID)))
		if ok := q.internalQueue.Enqueue(j); !ok {
			j.close()
			continue
		}

		q.postEnqueue(j)
	}

	return groupJob
}
