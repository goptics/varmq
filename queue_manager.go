package varmq

import (
	"errors"

	"github.com/goptics/varmq/internal/helpers"
)

type Strategy uint8

var errInvalidStrategyType = errors.New("invalid strategy type")

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

func newQueueManager(strategy Strategy) *queueManager {
	return &queueManager{
		Manager:  helpers.NewManager[IBaseQueue](),
		strategy: strategy,
	}
}

func (qm *queueManager) next() (IBaseQueue, error) {
	switch qm.strategy {
	case RoundRobin:
		return qm.GetRoundRobinItem()
	case MaxLen:
		return qm.GetMaxLenItem()
	case MinLen:
		return qm.GetMinLenItem()
	case Priority:
		return qm.GetPriorityItem()
	default:
		return nil, errInvalidStrategyType
	}
}
