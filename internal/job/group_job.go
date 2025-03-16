package job

import (
	"sync"
	"sync/atomic"

	"github.com/fahimfaisaal/gocq/v2/types"
)

// GroupJob represents a job that can be used in a group.
type GroupJob[T, R any] struct {
	*Job[T, R]
	result chan types.Result[R]
	wg     sync.WaitGroup
}

func NewGroupJob[T, R any](bufferSize uint32) *GroupJob[T, R] {
	return &GroupJob[T, R]{
		Job: &Job[T, R]{
			ResultChannel: NewResultChannel[R](bufferSize),
			status:        atomic.Uint32{},
		},
	}
}

func NewGroupVoidJob[T any](bufferSize uint32) *GroupJob[T, any] {
	return &GroupJob[T, any]{
		Job: &Job[T, any]{
			ResultChannel: NewVoidResultChannel(),
			status:        atomic.Uint32{},
		},
	}
}

func (gj *GroupJob[T, R]) NewJob(data T) *Job[T, R] {
	return &Job[T, R]{
		Data:          data,
		ResultChannel: gj.ResultChannel,
		status:        atomic.Uint32{},
	}
}

func (gj *GroupJob[T, R]) FanInResult(l int) *GroupJob[T, R] {
	gj.result = make(chan types.Result[R], l)

	gj.wg.Add(cap(gj.result))
	go func() {
		for {
			select {
			case val, ok := <-gj.ResultChannel.Data:
				if ok {
					gj.result <- types.Result[R]{Data: val}
					gj.wg.Done()
				} else {
					return
				}
			case err, ok := <-gj.ResultChannel.Err:
				if ok {
					gj.result <- types.Result[R]{Err: err}
					gj.wg.Done()
				} else {
					return
				}
			}
		}
	}()

	go func() {
		gj.wg.Wait()

		gj.ResultChannel.Close()
		close(gj.result)
	}()

	return gj
}

func (gj *GroupJob[T, R]) FanInVoidResult(l int) *GroupJob[T, R] {
	gj.result = make(chan types.Result[R], l)

	gj.wg.Add(cap(gj.result))
	go func() {
		for e := range gj.ResultChannel.Err {
			gj.result <- types.Result[R]{Err: e}
			gj.wg.Done()
		}
	}()

	go func() {
		gj.wg.Wait()

		gj.ResultChannel.Close()
		close(gj.result)
	}()

	return gj
}

func (gj *GroupJob[T, R]) Drain() {
	go func() {
		for range gj.result {
			// drain
		}
	}()
}

func (gj *GroupJob[T, R]) Results() chan types.Result[R] {
	return gj.result
}

func (gj *GroupJob[T, R]) Errors() <-chan error {
	err := make(chan error, cap(gj.result))

	go func() {
		for r := range gj.result {
			err <- r.Err
		}

		close(err)
	}()

	return err
}
