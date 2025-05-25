package varmq

import (
	"fmt"
	"sync"

	"github.com/goptics/varmq/internal/helpers"
)

// groupJob represents a job that can be used in a group.
type groupJob[T any] struct {
	job[T]
	wgc *helpers.WgCounter
}

type PendingTracker interface {
	NumPending() int
}

type EnqueuedGroupJob interface {
	PendingTracker
	Awaitable
}

const groupIdPrefixed = "g:"

func newGroupJob[T any](bufferSize int) *groupJob[T] {
	gj := &groupJob[T]{
		job: job[T]{},
		wgc: helpers.NewWgCounter(bufferSize),
	}
	return gj
}

func generateGroupId(id string) string {
	return fmt.Sprintf("%s%s", groupIdPrefixed, id)
}

func (gj *groupJob[T]) newJob(payload T, config jobConfigs) *groupJob[T] {
	return &groupJob[T]{
		job: job[T]{
			id:      generateGroupId(config.Id),
			payload: payload,
		},
		wgc: gj.wgc,
	}
}

func (gj *groupJob[T]) NumPending() int {
	return gj.wgc.Count()
}

func (gj *groupJob[T]) Wait() {
	gj.wgc.Wait()
}

func (gj *groupJob[T]) Close() error {
	if err := gj.isCloseable(); err != nil {
		return err
	}

	gj.ack()
	gj.changeStatus(closed)
	gj.wgc.Done()

	return nil
}

type resultGroupJob[T, R any] struct {
	resultJob[T, R]
	wgc *helpers.WgCounter
}

func newResultGroupJob[T, R any](bufferSize int) *resultGroupJob[T, R] {
	gj := &resultGroupJob[T, R]{
		resultJob: resultJob[T, R]{
			Response: helpers.NewResponse[Result[R]](bufferSize),
		},
		wgc: helpers.NewWgCounter(bufferSize),
	}

	return gj
}

type EnqueuedResultGroupJob[R any] interface {
	Results() (<-chan Result[R], error)
	PendingTracker
	Awaitable
	Drainer
}

func (gj *resultGroupJob[T, R]) newJob(payload T, config jobConfigs) *resultGroupJob[T, R] {
	return &resultGroupJob[T, R]{
		resultJob: resultJob[T, R]{
			job: job[T]{
				id:      generateGroupId(config.Id),
				payload: payload,
			},
			Response: gj.Response,
		},
		wgc: gj.wgc,
	}
}

func (gj *resultGroupJob[T, R]) NumPending() int {
	return gj.wgc.Count()
}

func (gj *resultGroupJob[T, R]) Wait() {
	gj.wgc.Wait()
}

func (gj *resultGroupJob[T, R]) Results() (<-chan Result[R], error) {
	ch, err := gj.Response.Read()

	if err != nil {
		tempCh := make(chan Result[R], 1)
		close(tempCh)
		return tempCh, err
	}

	return ch, nil
}

func (gj *resultGroupJob[T, R]) Close() error {
	if err := gj.isCloseable(); err != nil {
		return err
	}

	gj.ack()
	gj.changeStatus(closed)
	gj.wgc.Done()

	if gj.wgc.Count() == 0 {
		gj.Response.Close()
	}

	return nil
}

type errorGroupJob[T any] struct {
	errorJob[T]
	wgc *helpers.WgCounter
}

type EnqueuedErrGroupJob interface {
	Errs() (<-chan error, error)
	PendingTracker
	Awaitable
	Drainer
}

func newErrorGroupJob[T any](bufferSize int) *errorGroupJob[T] {
	return &errorGroupJob[T]{
		errorJob: errorJob[T]{
			job: job[T]{
				wg: sync.WaitGroup{},
			},
			Response: helpers.NewResponse[error](bufferSize),
		},
		wgc: helpers.NewWgCounter(bufferSize),
	}
}

func (gj *errorGroupJob[T]) NumPending() int {
	return gj.wgc.Count()
}

func (gj *errorGroupJob[T]) Wait() {
	gj.wgc.Wait()
}

func (gj *errorGroupJob[T]) newJob(payload T, config jobConfigs) *errorGroupJob[T] {
	return &errorGroupJob[T]{
		errorJob: errorJob[T]{
			job: job[T]{
				id:      generateGroupId(config.Id),
				payload: payload,
			},
			Response: gj.Response,
		},
		wgc: gj.wgc,
	}
}

func (gj *errorGroupJob[T]) Errs() (<-chan error, error) {
	ch, err := gj.Response.Read()

	if err != nil {
		tempCh := make(chan error, 1)
		close(tempCh)
		return tempCh, err
	}

	return ch, nil
}

func (gj *errorGroupJob[T]) Close() error {
	if err := gj.isCloseable(); err != nil {
		return err
	}

	gj.ack()
	gj.changeStatus(closed)
	gj.wgc.Done()

	if gj.wgc.Count() == 0 {
		gj.Response.Close()
	}

	return nil
}
