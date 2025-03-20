package job

import (
	"errors"
	"fmt"
	"sync"

	"github.com/fahimfaisaal/gocq/v2/shared/types"
)

// groupJob represents a job that can be used in a group.
type groupJob[T, R any] struct {
	*job[T, R]
	wg *sync.WaitGroup
}

const GroupIdPrefixed = "g:"

type GroupJob[T, R any] interface {
	Job[T, R]
	types.EnqueuedGroupJob[R]
	NewJob(data T, id string) GroupJob[T, R]
}

func NewGroupJob[T, R any](bufferSize uint32) GroupJob[T, R] {
	gj := &groupJob[T, R]{
		job: &job[T, R]{
			resultChannel: NewResultChannel[R](bufferSize),
		},
		wg: new(sync.WaitGroup),
	}

	gj.wg.Add(int(bufferSize))

	return gj
}

func GenerateGroupId(id string) string {
	return fmt.Sprintf("%s%s", GroupIdPrefixed, id)
}

func (gj *groupJob[T, R]) NewJob(data T, id string) GroupJob[T, R] {
	return &groupJob[T, R]{
		job: &job[T, R]{
			id:            GenerateGroupId(id),
			Input:         data,
			resultChannel: gj.resultChannel,
		},
		wg: gj.wg,
	}
}

func (gj *groupJob[T, R]) Results() (<-chan types.Result[R], error) {
	ch := gj.resultChannel.Read()

	// Start a goroutine to close the channel when all jobs are done
	if ch != nil {
		go func() {
			gj.wg.Wait()
			gj.CloseResultChannel()
		}()

		return ch, nil
	}

	tempCh := make(chan types.Result[R], 1)
	close(tempCh)

	// return a closed channel
	return tempCh, errors.New("result channel has already been consumed")
}

// Drain discards the job's result and error values asynchronously.
// This is useful when you no longer need the results but want to ensure
// the channels are emptied.
func (gj *groupJob[T, R]) Drain() error {
	ch := gj.resultChannel.Read()

	if ch == nil {
		return errors.New("result channel has already been consumed")
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

func (gj *groupJob[T, R]) Close() error {
	if err := gj.isCloseable(); err != nil {
		return err
	}

	gj.wg.Done()
	gj.ChangeStatus(Closed)
	return nil
}
