package varmq

import (
	"errors"

	"github.com/goptics/varmq/internal/helpers"
)

// errNoReadySource is returned by sourceManager.Next when no registered
// source has a job. The scheduler treats it as "stop pulling", not an error.
var errNoReadySource = errors.New("no source has a ready job")

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
// MinLen, or Priority). Every worker owns one, created with the worker, and
// the scheduler always pulls through it.
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

// Next pulls from the source the strategy picks. errNoReadySource means every
// registered source is empty.
func (sm *sourceManager[JobType]) Next() (JobType, error) {
	src, ok := sm.next()
	if !ok {
		var zero JobType
		return zero, errNoReadySource
	}

	return src.Next()
}

var _ source[iJob[int]] = (*sourceManager[iJob[int]])(nil)
