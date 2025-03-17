package job

import (
	"sync"

	"github.com/fahimfaisaal/gocq/v2/types"
)

// GroupJob represents a job that can be used in a group.
type GroupJob[T, R any] struct {
	*Job[T, R]
	wg *sync.WaitGroup
}

type IGroupJob[T, R any] interface {
	IJob[T, R]
	types.EnqueuedGroupJob[R]
	NewJob(data T) IGroupJob[T, R]
	Done()
}

func NewGroupJob[T, R any](bufferSize uint32) IGroupJob[T, R] {
	gj := &GroupJob[T, R]{
		Job: &Job[T, R]{
			resultChannel: NewResultChannel[R](),
		},
		wg: new(sync.WaitGroup),
	}

	gj.wg.Add(int(bufferSize))

	return gj
}

func (gj *GroupJob[T, R]) NewJob(data T) IGroupJob[T, R] {
	return &GroupJob[T, R]{
		Job: &Job[T, R]{
			data:          data,
			resultChannel: gj.resultChannel,
		},
		wg: gj.wg,
	}
}

func (gj *GroupJob[T, R]) Results() chan types.Result[R] {
	// Start a goroutine to close the channel when all jobs are done
	go func() {
		gj.wg.Wait()
		gj.CloseResultChannel()
	}()
	return gj.resultChannel
}

func (gj *GroupJob[T, R]) Done() {
	gj.wg.Done()
}
