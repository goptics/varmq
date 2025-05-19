package varmq

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// groupJob represents a job that can be used in a group.
type groupJob[T, R any] struct {
	*job[T, R]
	wg  *sync.WaitGroup
	len *groupLen
}

type groupLen struct {
	len atomic.Uint32
}

func (gl *groupLen) Add() {
	gl.len.Add(1)
}

func (gl *groupLen) Sub() {
	gl.len.Add(^uint32(0))
}

func (gl *groupLen) Get() int {
	return int(gl.len.Load())
}

const groupIdPrefixed = "g:"

func newGroupJob[T, R any](bufferSize int) *groupJob[T, R] {
	gj := &groupJob[T, R]{
		job: &job[T, R]{
			resultChannel: newResultChannel[R](bufferSize),
		},
		wg:  new(sync.WaitGroup),
		len: new(groupLen),
	}

	gj.wg.Add(bufferSize)
	return gj
}

func generateGroupId(id string) string {
	return fmt.Sprintf("%s%s", groupIdPrefixed, id)
}

func (gj *groupJob[T, R]) NewJob(data T, config jobConfigs) *groupJob[T, R] {
	gj.len.Add()
	return &groupJob[T, R]{
		job: &job[T, R]{
			id:            generateGroupId(config.Id),
			Input:         data,
			resultChannel: gj.resultChannel,
		},
		wg:  gj.wg,
		len: gj.len,
	}
}

func (gj *groupJob[T, R]) Results() (<-chan Result[R], error) {
	ch, err := gj.resultChannel.Read()

	if err != nil {
		tempCh := make(chan Result[R], 1)
		close(tempCh)
		return tempCh, err
	}

	// Start a goroutine to close the channel when all jobs are done
	go func() {
		gj.wg.Wait()
		gj.CloseResultChannel()
	}()

	// return a closed channel
	return ch, nil
}

func (gj *groupJob[T, R]) Len() int {
	return gj.len.Get()
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

	go func() {
		gj.wg.Wait()
		gj.CloseResultChannel()
	}()

	return nil
}

func (gj *groupJob[T, R]) close() error {
	if err := gj.isCloseable(); err != nil {
		return err
	}

	gj.wg.Done()
	gj.Ack()
	gj.ChangeStatus(closed)
	gj.len.Sub()
	return nil
}
