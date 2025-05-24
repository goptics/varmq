package varmq

// queue is the base implementation of the Queue interface
// It contains an externalQueue for worker management and an internalQueue for job storage
type queue[T any] struct {
	*externalQueue
	internalQueue IQueue
}

type Queue[T any] interface {
	IExternalQueue
	// Add adds a new Job to the queue and returns a EnqueuedJob to handle the job.
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) (EnqueuedJob, bool)
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedGroupJob to handle the job.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedGroupJob
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
func newQueue[T any](worker *worker[T, iJob[T]], q IQueue) *queue[T] {
	worker.setQueue(q)

	return &queue[T]{
		externalQueue: newExternalQueue(worker),
		internalQueue: q,
	}
}

func (q *queue[T]) Add(data T, configs ...JobConfigFunc) (EnqueuedJob, bool) {
	j := newJob(data, loadJobConfigs(q.w.configs(), configs...))

	if ok := q.internalQueue.Enqueue(j); !ok {
		j.close()
		return nil, false
	}

	j.changeStatus(queued)
	q.w.notifyToPullNextJobs()

	return j, true
}

func (q *queue[T]) AddAll(items []Item[T]) EnqueuedGroupJob {
	groupJob := newGroupJob[T](len(items))

	for _, item := range items {
		j := groupJob.newJob(item.Value, loadJobConfigs(q.w.configs(), WithJobId(item.ID)))
		if ok := q.internalQueue.Enqueue(j); !ok {
			j.close()
			continue
		}

		j.changeStatus(queued)
		q.w.notifyToPullNextJobs()
	}

	return groupJob
}

type resultQueue[T, R any] struct {
	*externalQueue
	internalQueue IQueue
}

type ResultQueue[T, R any] interface {
	IExternalQueue
	// Add adds a new Job to the queue and returns a EnqueuedJob to handle the job.
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) (EnqueuedResultJob[R], bool)
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedGroupJob to handle the job.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedResultGroupJob[R]
}

func newResultQueue[T, R any](worker *worker[T, iResultJob[T, R]], q IQueue) *resultQueue[T, R] {
	worker.setQueue(q)

	return &resultQueue[T, R]{
		externalQueue: newExternalQueue(worker),
		internalQueue: q,
	}
}

func (q *resultQueue[T, R]) Add(data T, configs ...JobConfigFunc) (EnqueuedResultJob[R], bool) {
	j := newResultJob[T, R](data, loadJobConfigs(q.w.configs(), configs...))

	if ok := q.internalQueue.Enqueue(j); !ok {
		j.close()
		return nil, false
	}

	j.changeStatus(queued)
	q.w.notifyToPullNextJobs()

	return j, true
}

func (q *resultQueue[T, R]) AddAll(items []Item[T]) EnqueuedResultGroupJob[R] {
	groupJob := newResultGroupJob[T, R](len(items))

	for _, item := range items {
		j := groupJob.newJob(item.Value, loadJobConfigs(q.w.configs(), WithJobId(item.ID)))
		if ok := q.internalQueue.Enqueue(j); !ok {
			j.close()
			continue
		}

		j.changeStatus(queued)
		q.w.notifyToPullNextJobs()
	}

	return groupJob
}
