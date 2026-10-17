package varmq

import "github.com/goptics/varmq/internal/helpers"

// groupJob represents a job that can be used in a group.
type groupJob[T any] struct {
	job[T]
	wgc *helpers.WgCounter
}

type PendingTracker interface {
	// NumPending returns the number of pending jobs in the group.
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
	return groupIdPrefixed + id
}

func (gj *groupJob[T]) newJob(data T, config jobConfigs) *groupJob[T] {
	return &groupJob[T]{
		job: job[T]{
			id:   generateGroupId(config.Id),
			data: data,
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

// resultGroupJob is one member of an AddAll group on a result queue. Unlike
// resultJob it fans results into a channel shared by the whole group, which
// is what Results() hands out.
type resultGroupJob[T, R any] struct {
	job[T]
	resp *helpers.Response[Result[R]]
	wgc  *helpers.WgCounter
}

// EnqueuedResultGroupJob is an interface representing a result group job that has been successfully enqueued.
// It extends PendingTracker, Awaitable, and Drainer interfaces to provide tracking, waiting, and
// cleanup capabilities. This interface is returned by the AddAll method when used with NewResultWorker,
// allowing clients to collect job results through the Results() method
type EnqueuedResultGroupJob[R any] interface {
	// Results returns a channel from which results of the group jobs can be received.
	// It blocks until all jobs in the group are completed.
	Results() <-chan Result[R]
	PendingTracker
	Awaitable
	Drainer
}

func newResultGroupJob[T, R any](bufferSize int) *resultGroupJob[T, R] {
	gj := &resultGroupJob[T, R]{
		resp: helpers.NewResponse[Result[R]](bufferSize),
		wgc:  helpers.NewWgCounter(bufferSize),
	}

	return gj
}

func (gj *resultGroupJob[T, R]) newJob(data T, config jobConfigs) *resultGroupJob[T, R] {
	return &resultGroupJob[T, R]{
		job: job[T]{
			id:   generateGroupId(config.Id),
			data: data,
		},
		resp: gj.resp,
		wgc:  gj.wgc,
	}
}

func (gj *resultGroupJob[T, R]) sendResult(result R) {
	gj.resp.Send(Result[R]{JobId: gj.id, Data: result})
}

func (gj *resultGroupJob[T, R]) sendError(err error) {
	gj.resp.Send(Result[R]{JobId: gj.id, Err: err})
}

func (gj *resultGroupJob[T, R]) Drain() {
	gj.resp.Drain()
}

func (gj *resultGroupJob[T, R]) NumPending() int {
	return gj.wgc.Count()
}

func (gj *resultGroupJob[T, R]) Wait() {
	gj.wgc.Wait()
}

func (gj *resultGroupJob[T, R]) Results() <-chan Result[R] {
	return gj.resp.Read()
}

func (gj *resultGroupJob[T, R]) Close() error {
	if err := gj.isCloseable(); err != nil {
		return err
	}

	gj.ack()
	gj.changeStatus(closed)
	gj.wgc.Done()

	if gj.wgc.Count() == 0 {
		gj.resp.Close()
	}

	return nil
}

// errorGroupJob is one member of an AddAll group on an error queue; see
// resultGroupJob.
type errorGroupJob[T any] struct {
	job[T]
	resp *helpers.Response[error]
	wgc  *helpers.WgCounter
}

// EnqueuedErrGroupJob is an interface representing an error group job that has been successfully enqueued.
// It extends PendingTracker, Awaitable, and Drainer interfaces to provide tracking, waiting, and
// cleanup capabilities. This interface is returned by the AddAll method when used with NewErrWorker,
// allowing clients to collect errors generated during job processing through the Errs() method
type EnqueuedErrGroupJob interface {
	// Errs returns a channel from which errors of the group jobs can be received.
	// It blocks until all jobs in the group are completed.
	Errs() <-chan error
	PendingTracker
	Awaitable
	Drainer
}

func newErrorGroupJob[T any](bufferSize int) *errorGroupJob[T] {
	return &errorGroupJob[T]{
		resp: helpers.NewResponse[error](bufferSize),
		wgc:  helpers.NewWgCounter(bufferSize),
	}
}

func (gj *errorGroupJob[T]) NumPending() int {
	return gj.wgc.Count()
}

func (gj *errorGroupJob[T]) Wait() {
	gj.wgc.Wait()
}

func (gj *errorGroupJob[T]) newJob(data T, config jobConfigs) *errorGroupJob[T] {
	return &errorGroupJob[T]{
		job: job[T]{
			id:   generateGroupId(config.Id),
			data: data,
		},
		resp: gj.resp,
		wgc:  gj.wgc,
	}
}

func (gj *errorGroupJob[T]) sendError(err error) {
	gj.resp.Send(err)
}

func (gj *errorGroupJob[T]) Drain() {
	gj.resp.Drain()
}

func (gj *errorGroupJob[T]) Errs() <-chan error {
	return gj.resp.Read()
}

func (gj *errorGroupJob[T]) Close() error {
	if err := gj.isCloseable(); err != nil {
		return err
	}

	gj.ack()
	gj.changeStatus(closed)
	gj.wgc.Done()

	if gj.wgc.Count() == 0 {
		gj.resp.Close()
	}

	return nil
}
