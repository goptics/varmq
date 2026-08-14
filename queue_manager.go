package varmq

import (
	"github.com/goptics/varmq/internal/helpers"
)

type Strategy uint8

const (
	// Selects queues in a round-robin fashion
	RoundRobin Strategy = iota
	// Selects the queue with the most items
	MaxLen
	// Selects the queue with the fewest items
	MinLen
	// Selects the queue with the highest priority first
	Priority
)

// queueManager manages multiple queues bound to a worker and selects the appropriate queue
// based on the configured strategy (RoundRobin, MaxLen, MinLen, or Priority)
type queueManager struct {
	*helpers.Manager[IBaseQueue]
	strategy Strategy
}

type queueSnapshot struct {
	Len int
}

func newQueueManager(strategy Strategy) *queueManager {
	return &queueManager{
		Manager:  helpers.NewManager[IBaseQueue](),
		strategy: strategy,
	}
}

func (qm *queueManager) next() (IBaseQueue, bool) {
	switch qm.strategy {
	case RoundRobin:
		return qm.GetRoundRobinItem()
	case MaxLen:
		return qm.GetMaxLenItem()
	case MinLen:
		return qm.GetMinLenItem()
	default:
		return qm.GetPriorityItem()
	}
}

func (qm *queueManager) snapshot() []queueSnapshot {
	queues := qm.Snapshot()
	snapshot := make([]queueSnapshot, len(queues))
	for i, queue := range queues {
		snapshot[i].Len = queue.Len()
	}

	return snapshot
}

func (w *worker[T, JobType]) numQueues() int {
	return w.queues.Count()
}

func (w *worker[T, JobType]) queueSnapshot() []queueSnapshot {
	return w.queues.snapshot()
}
