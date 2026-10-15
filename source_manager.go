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

// sourceManager is what a worker pulls jobs from. Which implementation is
// current depends on how many sources are bound:
//
//   - emptyManager: nothing bound; there is never a job.
//   - directManager: one source; pulls from it directly, with no strategy.
//   - strategyManager: two or more; picks a source by the worker's Strategy.
//
// add and remove return the manager for the new set of sources, so the
// worker swaps implementations as sources come and go, in both directions.
type sourceManager[JobType any] interface {
	source[JobType]
	add(src source[JobType], priority int, strategy Strategy) sourceManager[JobType]
	remove(src source[JobType]) sourceManager[JobType]
	count() int
}

// newSourceManager returns the manager of a worker with nothing bound yet.
func newSourceManager[JobType any]() sourceManager[JobType] {
	return &emptyManager[JobType]{}
}

// emptyManager is the manager of a worker with no bound source.
type emptyManager[JobType any] struct{}

func (m *emptyManager[JobType]) Len() int { return 0 }

func (m *emptyManager[JobType]) Next() (JobType, error) {
	var zero JobType
	return zero, errNoReadySource
}

func (m *emptyManager[JobType]) add(src source[JobType], priority int, _ Strategy) sourceManager[JobType] {
	return &directManager[JobType]{src: src, priority: priority}
}

func (m *emptyManager[JobType]) remove(source[JobType]) sourceManager[JobType] { return m }

func (m *emptyManager[JobType]) count() int { return 0 }

// directManager is the manager of a worker with exactly one bound source. It
// forwards to that source; there is nothing to choose between.
type directManager[JobType any] struct {
	src      source[JobType]
	priority int // kept so the source keeps its rank if a second one is bound
}

func (m *directManager[JobType]) Len() int { return m.src.Len() }

// Next reports an empty source as errNoReadySource, as strategyManager's
// pick does, so the scheduler treats it as "nothing to do". Without the
// check the source would return ErrFailedToDequeue, which the event loop
// reports as a failure; storage cannot tell "empty" from "failed" itself.
func (m *directManager[JobType]) Next() (JobType, error) {
	if m.src.Len() == 0 {
		var zero JobType
		return zero, errNoReadySource
	}

	return m.src.Next()
}

func (m *directManager[JobType]) add(src source[JobType], priority int, strategy Strategy) sourceManager[JobType] {
	s := newStrategyManager[JobType](strategy)
	s.Register(m.src, m.priority)
	s.Register(src, priority)

	return s
}

func (m *directManager[JobType]) remove(src source[JobType]) sourceManager[JobType] {
	if src != m.src {
		return m
	}

	return &emptyManager[JobType]{}
}

func (m *directManager[JobType]) count() int { return 1 }

// strategyManager is the manager of a worker with two or more bound sources.
// A remove leaves at least one, so it only ever steps down to directManager.
// It picks the next source by strategy (RoundRobin, MaxLen, MinLen, or
// Priority).
type strategyManager[JobType any] struct {
	*helpers.Manager[source[JobType]]
	strategy Strategy
}

func newStrategyManager[JobType any](strategy Strategy) *strategyManager[JobType] {
	return &strategyManager[JobType]{
		Manager:  helpers.NewManager[source[JobType]](),
		strategy: strategy,
	}
}

func (sm *strategyManager[JobType]) next() (source[JobType], bool) {
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
func (sm *strategyManager[JobType]) Next() (JobType, error) {
	src, ok := sm.next()
	if !ok {
		var zero JobType
		return zero, errNoReadySource
	}

	return src.Next()
}

func (sm *strategyManager[JobType]) add(src source[JobType], priority int, _ Strategy) sourceManager[JobType] {
	sm.Register(src, priority)
	return sm
}

func (sm *strategyManager[JobType]) remove(src source[JobType]) sourceManager[JobType] {
	sm.UnregisterItem(src)

	if only, priority, ok := sm.Only(); ok {
		return &directManager[JobType]{src: only, priority: priority}
	}

	return sm
}

func (sm *strategyManager[JobType]) count() int { return sm.Count() }

var (
	_ sourceManager[iJob[int]] = (*emptyManager[iJob[int]])(nil)
	_ sourceManager[iJob[int]] = (*directManager[iJob[int]])(nil)
	_ sourceManager[iJob[int]] = (*strategyManager[iJob[int]])(nil)
)
