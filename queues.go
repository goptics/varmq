package gocq

import (
	"sync"

	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
)

type Queues[T, R any] interface {
	Worker[T, R]
	BindQueue() ConcurrentQueue[T, R]
	BindPriorityQueue() ConcurrentPriorityQueue[T, R]
	BindWithPersistentQueue(pq types.IQueue) ConcurrentPersistentQueue[T, R]
	BindWithDistributedQueue(dq types.IDistributedQueue) (DistributedQueue[T, R], error)
}

type queues[T, R any] struct {
	*worker[T, R]
}

func newQueues[T, R any](worker *worker[T, R]) Queues[T, R] {
	return &queues[T, R]{
		worker: worker,
	}
}

func (q *queues[T, R]) BindQueue() ConcurrentQueue[T, R] {
	defer q.worker.start()

	q.worker.setQueue(queue.NewQueue[job.Job[T, R]]())
	return newQueue[T, R](q.worker)
}

func (q *queues[T, R]) BindPriorityQueue() ConcurrentPriorityQueue[T, R] {
	defer q.worker.start()
	q.worker.setQueue(queue.NewPriorityQueue[job.Job[T, R]]())
	return newPriorityQueue[T, R](q.worker)
}

func (q *queues[T, R]) BindWithPersistentQueue(pq types.IQueue) ConcurrentPersistentQueue[T, R] {
	defer q.worker.start()
	q.worker.setQueue(pq)

	// if cache is not set, use sync.Map as the default cache, we need it for persistent queue
	if q.worker.Cache == getCache() {
		q.worker.Cache = new(sync.Map)
	}

	return newPersistentQueue[T, R](q.worker)
}

func (q *queues[T, R]) BindWithDistributedQueue(dq types.IDistributedQueue) (_ DistributedQueue[T, R], err error) {
	queue := NewDistributedQueue[T, R](dq)
	defer func() {
		if err != nil {
			queue.Close()
		} else {
			go queue.listenEnqueueNotification(func() {
				q.worker.sync.wg.Add(1)
				q.worker.jobPullNotifier.Notify()
			})
		}
	}()
	defer func() {
		err = q.worker.start()
	}()

	q.worker.setQueue(dq)
	return queue, err
}
