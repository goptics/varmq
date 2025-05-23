package varmq

import (
	"fmt"
	"sync/atomic"
)

// groupJob represents a job that can be used in a group.
type groupJob[T, R any] struct {
	job[T, R]
	done chan struct{}
	len  *atomic.Uint32
}

const groupIdPrefixed = "g:"

func newGroupJob[T, R any](bufferSize int) *groupJob[T, R] {
	gj := &groupJob[T, R]{
		job: job[T, R]{
			resultChannel: newResultChannel[R](bufferSize),
		},
		done: make(chan struct{}),
		len:  new(atomic.Uint32),
	}

	gj.len.Add(uint32(bufferSize))

	return gj
}

func generateGroupId(id string) string {
	return fmt.Sprintf("%s%s", groupIdPrefixed, id)
}

func (gj *groupJob[T, R]) newJob(data T, config jobConfigs) *groupJob[T, R] {
	return &groupJob[T, R]{
		job: job[T, R]{
			id:            generateGroupId(config.Id),
			Input:         data,
			resultChannel: gj.resultChannel,
		},
		done: gj.done,
		len:  gj.len,
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
	<-gj.done
}

func (gj *groupJob[T, R]) Len() int {
	return int(gj.len.Load())
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

	gj.Ack()
	gj.ChangeStatus(closed)

	// Close the result channel if all jobs are done
	if gj.len.Add(^uint32(0)) == 0 {
		close(gj.done)
		gj.CloseResultChannel()
	}
	return nil
}
