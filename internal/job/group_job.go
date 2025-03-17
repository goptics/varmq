package job

import (
	"sync"

	"github.com/fahimfaisaal/gocq/v2/types"
)

// groupJob represents a job that can be used in a group.
type groupJob[T, R any] struct {
	*job[T, R]
	wg *sync.WaitGroup
}

type GroupJob[T, R any] interface {
	Job[T, R]
	types.EnqueuedGroupJob[R]
	NewJob(data T) GroupJob[T, R]
	Done()
}

func NewGroupJob[T, R any](bufferSize uint32) GroupJob[T, R] {
	gj := &groupJob[T, R]{
		job: &job[T, R]{
			resultChannel: NewResultChannel[R](),
		},
		wg: new(sync.WaitGroup),
	}

	gj.wg.Add(int(bufferSize))

	return gj
}

func (gj *groupJob[T, R]) NewJob(data T) GroupJob[T, R] {
	return &groupJob[T, R]{
		job: &job[T, R]{
			data:          data,
			resultChannel: gj.resultChannel,
		},
		wg: gj.wg,
	}
}

func (gj *groupJob[T, R]) Results() chan types.Result[R] {
	// Start a goroutine to close the channel when all jobs are done
	go func() {
		gj.wg.Wait()
		gj.CloseResultChannel()
	}()
	return gj.resultChannel
}

func (gj *groupJob[T, R]) Done() {
	gj.wg.Done()
}
