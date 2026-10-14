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

// sourceManager holds every source bound to a worker and picks the next one
// to pull from according to the configured strategy (RoundRobin, MaxLen,
// MinLen, or Priority).
type sourceManager[JobType any] struct {
	*helpers.Manager[source[JobType]]
	strategy Strategy
}

func newSourceManager[JobType any](strategy Strategy) *sourceManager[JobType] {
	return &sourceManager[JobType]{
		Manager:  helpers.NewManager[source[JobType]](),
		strategy: strategy,
	}
}

func (sm *sourceManager[JobType]) next() (source[JobType], bool) {
	switch sm.strategy {
	case RoundRobin:
		return sm.GetRoundRobinItem()
	case MaxLen:
		return sm.GetMaxLenItem()
	case MinLen:
		return sm.GetMinLenItem()
	default:
		return sm.GetPriorityItem()
	}
}
