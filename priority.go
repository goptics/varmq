package varmq

type priorityQueue[T any] struct {
	*externalQueue
	internalQueue IPriorityQueue
}

type PriorityQueue[T any] interface {
	IExternalQueue
	// Add adds a new Job with the given priority to the queue and returns a EnqueuedJob to handle the job.
	// Time complexity: O(log n)
	Add(data T, priority int, configs ...JobConfigFunc) (EnqueuedJob, bool)
	// AddAll adds multiple Jobs with the given priority to the queue and returns a EnqueuedGroupJob to handle the job.
	// Time complexity: O(n log n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedGroupJob
}

// NewPriorityQueue creates a new priorityQueue with the specified concurrency and worker function.
func newPriorityQueue[T any](worker *worker[T, iJob[T]], pq IPriorityQueue) *priorityQueue[T] {
	worker.setQueue(pq)

	return &priorityQueue[T]{
		externalQueue: newExternalQueue(worker),
		internalQueue: pq,
	}
}

func (q *priorityQueue[T]) Add(data T, priority int, configs ...JobConfigFunc) (EnqueuedJob, bool) {
	j := newJob(data, loadJobConfigs(q.w.configs(), configs...))

	if ok := q.internalQueue.Enqueue(j, priority); !ok {
		j.Close()
		return nil, false
	}

	j.changeStatus(queued)
	q.w.notifyToPullNextJobs()

	return j, true
}

func (q *priorityQueue[T]) AddAll(items []Item[T]) EnqueuedGroupJob {
	groupJob := newGroupJob[T](len(items))

	for _, item := range items {
		j := groupJob.newJob(item.Value, loadJobConfigs(q.w.configs(), WithJobId(item.ID)))

		if ok := q.internalQueue.Enqueue(j, item.Priority); !ok {
			j.Close()
			continue
		}

		j.changeStatus(queued)
		q.w.notifyToPullNextJobs()
	}

	return groupJob
}

type resultPriorityQueue[T any, R any] struct {
	*externalQueue
	internalQueue IPriorityQueue
}

type ResultPriorityQueue[T, R any] interface {
	IExternalQueue
	// Add adds a new Job with the given priority to the queue and returns a EnqueuedResultJob to handle the job with result receiving.
	// Time complexity: O(log n)
	Add(data T, priority int, configs ...JobConfigFunc) (EnqueuedResultJob[R], bool)
	// AddAll adds multiple Jobs with the given priority to the queue and returns a EnqueuedResultGroupJob to handle the job with result receiving.
	// Time complexity: O(n log n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedResultGroupJob[R]
}

func newResultPriorityQueue[T, R any](worker *worker[T, iResultJob[T, R]], pq IPriorityQueue) *resultPriorityQueue[T, R] {
	worker.setQueue(pq)

	return &resultPriorityQueue[T, R]{
		externalQueue: newExternalQueue(worker),
		internalQueue: pq,
	}
}

func (q *resultPriorityQueue[T, R]) Add(data T, priority int, configs ...JobConfigFunc) (EnqueuedResultJob[R], bool) {
	j := newResultJob[T, R](data, loadJobConfigs(q.w.configs(), configs...))

	if ok := q.internalQueue.Enqueue(j, priority); !ok {
		j.Close()
		return nil, false
	}

	j.changeStatus(queued)
	q.w.notifyToPullNextJobs()

	return j, true
}

func (q *resultPriorityQueue[T, R]) AddAll(items []Item[T]) EnqueuedResultGroupJob[R] {
	groupJob := newResultGroupJob[T, R](len(items))

	for _, item := range items {
		j := groupJob.newJob(item.Value, loadJobConfigs(q.w.configs(), WithJobId(item.ID)))

		if ok := q.internalQueue.Enqueue(j, item.Priority); !ok {
			j.Close()
			continue
		}

		j.changeStatus(queued)
		q.w.notifyToPullNextJobs()
	}

	return groupJob
}

type errorPriorityQueue[T any] struct {
	*externalQueue
	internalQueue IPriorityQueue
}

type ErrPriorityQueue[T any] interface {
	IExternalQueue
	// Add adds a new Job with the given priority to the queue and returns a EnqueuedErrJob to handle the job with error receiving.
	// Time complexity: O(log n)
	Add(data T, priority int, configs ...JobConfigFunc) (EnqueuedErrJob, bool)
	// AddAll adds multiple Jobs with the given priority to the queue and returns a EnqueuedErrGroupJob to handle the job with error receiving.
	// Time complexity: O(n log n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedErrGroupJob
}

func newErrorPriorityQueue[T any](worker *worker[T, iErrorJob[T]], pq IPriorityQueue) *errorPriorityQueue[T] {
	worker.setQueue(pq)

	return &errorPriorityQueue[T]{
		externalQueue: newExternalQueue(worker),
		internalQueue: pq,
	}
}

func (q *errorPriorityQueue[T]) Add(data T, priority int, configs ...JobConfigFunc) (EnqueuedErrJob, bool) {
	j := newErrorJob(data, loadJobConfigs(q.w.configs(), configs...))

	if ok := q.internalQueue.Enqueue(j, priority); !ok {
		j.Close()
		return nil, false
	}

	j.changeStatus(queued)
	q.w.notifyToPullNextJobs()

	return j, true
}

func (q *errorPriorityQueue[T]) AddAll(items []Item[T]) EnqueuedErrGroupJob {
	groupJob := newErrorGroupJob[T](len(items))

	for _, item := range items {
		j := groupJob.newJob(item.Value, loadJobConfigs(q.w.configs(), WithJobId(item.ID)))

		if ok := q.internalQueue.Enqueue(j, item.Priority); !ok {
			j.Close()
			continue
		}

		j.changeStatus(queued)
		q.w.notifyToPullNextJobs()
	}

	return groupJob
}
