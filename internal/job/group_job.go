package job

import (
	"sync"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/v2/types"
)

// GroupJob represents a job that can be used in a group.
type GroupJob[T, R any] struct {
	*Job[T, R]
	wg       sync.WaitGroup
	jobCount atomic.Uint32
}

type IGroupJob[T, R any] interface {
	IJob[T, R]
	NewJob(data T) IGroupJob[T, R]
	Drain()
	Results() chan types.Result[R]
	Done()
}

func NewGroupJob[T, R any](bufferSize uint32) IGroupJob[T, R] {
	gj := &GroupJob[T, R]{
		Job: &Job[T, R]{
			resultChannel: NewResultChannel[R](bufferSize),
			status:        atomic.Uint32{},
		},
	}
	gj.jobCount.Store(bufferSize)

	return gj
}

func (gj *GroupJob[T, R]) NewJob(data T) IGroupJob[T, R] {
	return &GroupJob[T, R]{
		Job: &Job[T, R]{
			data:          data,
			resultChannel: gj.resultChannel,
			status:        atomic.Uint32{},
		},
	}
}

func (gj *GroupJob[T, R]) Drain() {
	go func() {
		for range gj.Job.resultChannel {
			// drain
		}
	}()
}

func (gj *GroupJob[T, R]) Results() chan types.Result[R] {
	return gj.Job.resultChannel
}

func (gj *GroupJob[T, R]) Done() {
	gj.wg.Done()
	gj.jobCount.Store(gj.jobCount.Load() - 1)

	if gj.jobCount.Load() == 0 {
		gj.Job.CloseResultChannel()
	}
}
