package gocq

type queue[T, R any] struct {
	*externalQueue[T, R]
	internalQueue IQueue
}

type Queue[T, R any] interface {
	IExternalQueue[T, R]
	// Add adds a new Job to the queue and returns a EnqueuedJob to handle the
	// Time complexity: O(1)
	Add(data T, configs ...JobConfigFunc) EnqueuedJob[R]
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedGroupJob to handle the
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) EnqueuedGroupJob[R]
}

type Item[T any] struct {
	ID    string
	Value T
}

// Creates a new CQueue with the specified concurrency and worker function.
// Internally it calls Init() to start the worker goroutines based on the concurrency.
func newQueue[T, R any](worker *worker[T, R], q IQueue) *queue[T, R] {
	worker.setQueue(q)

	return &queue[T, R]{
		externalQueue: newExternalQueue(worker),
		internalQueue: q,
	}
}

func (q *queue[T, R]) Add(data T, configs ...JobConfigFunc) EnqueuedJob[R] {
	j := newJob[T, R](data, loadJobConfigs(q.configs, configs...))

	q.internalQueue.Enqueue(j)
	q.postEnqueue(j)

	return j
}

func (q *queue[T, R]) AddAll(items []Item[T]) EnqueuedGroupJob[R] {
	l := len(items)
	groupJob := newGroupJob[T, R](uint32(l))

	for _, item := range items {
		j := groupJob.NewJob(item.Value, loadJobConfigs(q.configs, WithJobId(item.ID)))
		q.internalQueue.Enqueue(j)
		q.postEnqueue(j)
	}

	return groupJob
}
