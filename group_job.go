package varmq

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// groupControl combines synchronization and counting for a group job.
type groupControl struct {
	wg  sync.WaitGroup
	len atomic.Uint32
}

// groupJob represents a job that can be used in a group.
type groupJob[T, R any] struct {
	*job[T, R]
	ctrl *groupControl
}

const groupIdPrefixed = "g:"

func (gc *groupControl) SubJob() uint32 {
	return gc.len.Add(^uint32(0))
}

func (gc *groupControl) JobCount() int {
	return int(gc.len.Load())
}

func (gc *groupControl) AddWait(delta int) {
	gc.wg.Add(delta)
}

func (gc *groupControl) Done() {
	gc.wg.Done()
}

func (gc *groupControl) Wait() {
	gc.wg.Wait()
}

func newGroupJob[T, R any](bufferSize int) *groupJob[T, R] {
	ctrl := new(groupControl)
	ctrl.AddWait(bufferSize)
	ctrl.len.Store(uint32(bufferSize))

	gj := &groupJob[T, R]{
		job: &job[T, R]{
			resultChannel: newResultChannel[R](bufferSize),
		},
		ctrl: ctrl,
	}

	return gj
}

func generateGroupId(id string) string {
	return fmt.Sprintf("%s%s", groupIdPrefixed, id)
}

func (gj *groupJob[T, R]) NewJob(data T, config jobConfigs) *groupJob[T, R] {
	return &groupJob[T, R]{
		job: &job[T, R]{
			id:            generateGroupId(config.Id),
			Input:         data,
			resultChannel: gj.resultChannel,
		},
		ctrl: gj.ctrl,
	}
}

func (gj *groupJob[T, R]) Results() (<-chan Result[R], error) {
	ch, err := gj.resultChannel.Read()

	if err != nil {
		tempCh := make(chan Result[R], 1)
		close(tempCh)
		return tempCh, err
	}

	return ch, nil
}

func (gj *groupJob[T, R]) Wait() {
	gj.ctrl.Wait()
}

func (gj *groupJob[T, R]) Len() int {
	return gj.ctrl.JobCount()
}

// Drain discards the job's result and error values asynchronously.
// This is useful when you no longer need the results but want to ensure
// the channels are emptied.
func (gj *groupJob[T, R]) Drain() error {
	ch, err := gj.resultChannel.Read()

	if ch != nil {
		return err
	}

	go func() {
		for range ch {
			// drain
		}
	}()

	return nil
}

func (gj *groupJob[T, R]) close() error {
	if err := gj.isCloseable(); err != nil {
		return err
	}

	gj.ctrl.Done()
	gj.Ack()
	gj.ChangeStatus(closed)

	// Close the result channel if all jobs are done
	if gj.ctrl.SubJob() == 0 {
		gj.CloseResultChannel()
	}
	return nil
}
