package gocq

import (
	"fmt"
	"strings"

	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
)

type concurrentQueue[T, R any] struct {
	*worker[T, R]
	queue types.IQueue
}

type ConcurrentQueue[T, R any] interface {
	ICQueue[R]
	// Add adds a new Job to the queue and returns a EnqueuedJob to handle the job.
	// Time complexity: O(1)
	Add(data T, id ...string) types.EnqueuedJob[R]
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedGroupJob to handle the job.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []Item[T]) types.EnqueuedGroupJob[R]
}

type Item[T any] struct {
	ID    string
	Value T
}

// Creates a new CQueue with the specified concurrency and worker function.
// Internally it calls Init() to start the worker goroutines based on the concurrency.
func newQueue[T, R any](worker *worker[T, R]) *concurrentQueue[T, R] {
	q := queue.NewQueue[job.Job[T, R]]()
	worker.setQueue(q)

	return &concurrentQueue[T, R]{
		worker: worker,
		queue:  q,
	}
}

func (q *concurrentQueue[T, R]) PendingCount() int {
	return q.queue.Len()
}

func (q *concurrentQueue[T, R]) postEnqueue(j job.Job[T, R]) {
	defer q.notify()
	j.ChangeStatus(job.Queued)

	if id := j.ID(); id != "" {
		q.Cache.Store(id, j)
	}
}

func (q *concurrentQueue[T, R]) JobById(id string) (types.EnqueuedJob[R], error) {
	val, ok := q.Cache.Load(id)
	if !ok {
		return nil, fmt.Errorf("job not found for id: %s", id)
	}

	return val.(types.EnqueuedJob[R]), nil
}

func (q *concurrentQueue[T, R]) GroupsJobById(id string) (types.EnqueuedSingleGroupJob[R], error) {
	if !strings.HasPrefix(id, job.GroupIdPrefixed) {
		id = job.GenerateGroupId(id)
	}

	val, ok := q.Cache.Load(id)

	if !ok {
		return nil, fmt.Errorf("groups job not found for id: %s", id)
	}

	return val.(types.EnqueuedSingleGroupJob[R]), nil
}

func (q *concurrentQueue[T, R]) Add(data T, id ...string) types.EnqueuedJob[R] {
	j := job.New[T, R](data, id...)

	q.queue.Enqueue(j)
	q.sync.wg.Add(1)
	q.postEnqueue(j)

	return j
}

func (q *concurrentQueue[T, R]) AddAll(items []Item[T]) types.EnqueuedGroupJob[R] {
	l := len(items)
	groupJob := job.NewGroupJob[T, R](uint32(l))

	q.sync.wg.Add(l)
	for _, item := range items {
		j := groupJob.NewJob(item.Value, item.ID)
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
	q.Queue.Close()
	q.sync.wg.Add(-len(prevValues))

	// close all pending channels to avoid routine leaks
	for _, val := range prevValues {
		if j, ok := val.(job.Job[T, R]); ok {
			j.CloseResultChannel()
		}
	}
}

func (q *concurrentQueue[T, R]) Close() error {
	q.Purge()
	err := q.Queue.Close()
	q.WaitUntilFinished()
	q.Stop()

	return err
}

func (q *concurrentQueue[T, R]) WaitAndClose() error {
	q.WaitUntilFinished()
	return q.Close()
}
