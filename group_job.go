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

// EnqueuedGroupJob is an interface representing a group job that has been successfully enqueued.
// It extends PendingTracker and Awaitable
// This interface is returned by the AddAll method when used with NewWorker
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

// EnqueuedResultGroupJob is an interface representing a result group job that has been successfully enqueued.
// It extends PendingTracker, Awaitable, and Drainer interfaces to provide tracking, waiting, and
// cleanup capabilities. This interface is returned by the AddAll method when used with NewResultWorker,
// allowing clients to collect job results through the Results() method
type EnqueuedResultGroupJob[R any] interface {
	Results() <-chan Result[R]
	PendingTracker
	Awaitable
	Drainer
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

func (gj *resultGroupJob[T, R]) Results() <-chan Result[R] {
	return gj.Response.Read()
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

// EnqueuedErrGroupJob is an interface representing an error group job that has been successfully enqueued.
// It extends PendingTracker, Awaitable, and Drainer interfaces to provide tracking, waiting, and
// cleanup capabilities. This interface is returned by the AddAll method when used with NewErrWorker,
// allowing clients to collect errors generated during job processing through the Errs() method
type EnqueuedErrGroupJob interface {
	Errs() <-chan error
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

func (gj *errorGroupJob[T]) Errs() <-chan error {
	return gj.Response.Read()
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
