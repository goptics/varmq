package job

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/fahimfaisaal/gocq/v2/types"
)

// groupJob represents a job that can be used in a group.
type groupJob[T, R any] struct {
	*job[T, R]
	wg *sync.WaitGroup
}

const groupIdPrefixed = "g:"

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
	return fmt.Sprintf("%s%s", groupIdPrefixed, id)
}

func (gj *groupJob[T, R]) NewJob(data T, id string) GroupJob[T, R] {
	return &groupJob[T, R]{
		job: &job[T, R]{
			id:            GenerateGroupId(id),
			data:          data,
			resultChannel: gj.resultChannel,
		},
		wg: gj.wg,
	}
}

func (gj *groupJob[T, R]) ID() string {
	after, ok := strings.CutPrefix(gj.id, groupIdPrefixed)
	if !ok {
		return gj.id
	}

	return after
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

	tempCh := make(chan types.Result[R], 0)
	close(tempCh)

	// return a closed channel
	return tempCh, errors.New("multiple calls to Results()")
}

func (gj *groupJob[T, R]) Close() error {
	if err := gj.isCloseable(); err != nil {
		return err
	}

	gj.wg.Done()
	gj.ChangeStatus(Closed)
	return nil
}
