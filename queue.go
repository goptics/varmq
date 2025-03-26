package gocq

import (
	"fmt"
	"strings"

	"github.com/fahimfaisaal/gocq/v3/internal/queue"
)

type concurrentQueue[T, R any] struct {
	*worker[T, R]
	queue IQueue
}

type Queue[T, R any] interface {
	ICQueue[T, R]
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
func newQueue[T, R any](worker *worker[T, R]) *concurrentQueue[T, R] {
	q := queue.NewQueue[iJob[T, R]]()

	worker.setQueue(q)

	return &concurrentQueue[T, R]{
		worker: worker,
		queue:  q,
	}
}

func (q *concurrentQueue[T, R]) postEnqueue(j iJob[T, R]) {
	defer q.notifyToPullNextJobs()
	j.ChangeStatus(queued)

	if id := j.ID(); id != "" {
		q.Cache.Store(id, j)
	}
}

func (q *concurrentQueue[T, R]) PendingCount() int {
	return q.Queue.Len()
}

func (q *concurrentQueue[T, R]) Worker() Worker[T, R] {
	return q.worker
}

func (q *concurrentQueue[T, R]) JobById(id string) (EnqueuedJob[R], error) {
	val, ok := q.Cache.Load(id)
	if !ok {
		return nil, fmt.Errorf("job not found for id: %s", id)
	}

	return val.(EnqueuedJob[R]), nil
}

func (q *concurrentQueue[T, R]) GroupsJobById(id string) (EnqueuedSingleGroupJob[R], error) {
	if !strings.HasPrefix(id, groupIdPrefixed) {
		id = generateGroupId(id)
	}

	val, ok := q.Cache.Load(id)

	if !ok {
		return nil, fmt.Errorf("groups job not found for id: %s", id)
	}

	return val.(EnqueuedSingleGroupJob[R]), nil
}

func (q *concurrentQueue[T, R]) Add(data T, configs ...JobConfigFunc) EnqueuedJob[R] {
	j := newJob[T, R](data, loadJobConfigs(q.configs, configs...))

	q.queue.Enqueue(j)
	q.postEnqueue(j)

	return j
}

func (q *concurrentQueue[T, R]) AddAll(items []Item[T]) EnqueuedGroupJob[R] {
	l := len(items)
	groupJob := newGroupJob[T, R](uint32(l))

	for _, item := range items {
		j := groupJob.NewJob(item.Value, loadJobConfigs(q.configs, WithJobId(item.ID)))
		q.queue.Enqueue(j)
		q.postEnqueue(j)
	}

	return groupJob
}

func (q *concurrentQueue[T, R]) WaitUntilFinished() {
	q.sync.wg.Wait()
}

func (q *concurrentQueue[T, R]) Purge() {
	prevValues := q.Queue.Values()
	q.Queue.Purge()

	// close all pending channels to avoid routine leaks
	for _, val := range prevValues {
		if j, ok := val.(iJob[T, R]); ok {
			j.CloseResultChannel()
		}
	}
}

func (q *concurrentQueue[T, R]) Close() error {
	q.Purge()
	q.Stop()
	q.WaitUntilFinished()

	return nil
}

func (q *concurrentQueue[T, R]) WaitAndClose() error {
	q.WaitUntilFinished()
	return q.Close()
}
