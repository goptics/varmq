package varmq

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// groupJob represents a job that can be used in a group.
type groupJob[T any] struct {
	job[T]
	pending *atomic.Uint32
	done    chan struct{}
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
		job:     job[T]{},
		pending: new(atomic.Uint32),
		done:    make(chan struct{}),
	}

	gj.pending.Add(uint32(bufferSize))

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
		pending: gj.pending,
		done:    gj.done,
	}
}

func (gj *groupJob[T]) NumPending() int {
	return int(gj.pending.Load())
}

func (gj *groupJob[T]) close() error {
	if err := gj.isCloseable(); err != nil {
		return err
	}

	gj.ack()
	gj.changeStatus(closed)

	if gj.pending.Add(^uint32(0)) == 0 {
		close(gj.done)
	}

	return nil
}

type resultGroupJob[T, R any] struct {
	resultJob[T, R]
	pending *atomic.Uint32
	done    chan struct{}
}

func newResultGroupJob[T, R any](bufferSize int) *resultGroupJob[T, R] {
	gj := &resultGroupJob[T, R]{
		resultJob: resultJob[T, R]{
			job: job[T]{
				wg: sync.WaitGroup{},
			},
			ResultController: newResultController[R](bufferSize),
		},
		pending: new(atomic.Uint32),
		done:    make(chan struct{}),
	}

	gj.pending.Add(uint32(bufferSize))

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
			ResultController: gj.ResultController,
		},
		pending: gj.pending,
		done:    gj.done,
	}
}

func (gj *resultGroupJob[T, R]) NumPending() int {
	return int(gj.pending.Load())
}

func (gj *resultGroupJob[T, R]) Results() (<-chan Result[R], error) {
	ch, err := gj.ResultController.Read()

	if err != nil {
		tempCh := make(chan Result[R], 1)
		close(tempCh)
		return tempCh, err
	}

	return ch, nil
}

func (gj *resultGroupJob[T, R]) close() error {
	if err := gj.isCloseable(); err != nil {
		return err
	}

	gj.ack()
	gj.changeStatus(closed)

	// Close the result channel if all jobs are done
	if gj.pending.Add(^uint32(0)) == 0 {
		close(gj.done)
		gj.ResultController.Close()
	}

	return nil
}
