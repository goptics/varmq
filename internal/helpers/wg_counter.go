package helpers

import (
	"sync"
	"sync/atomic"
)

type WgCounter struct {
	count atomic.Uint32
	wg    sync.WaitGroup
}

func NewWgCounter(bufferSize int) *WgCounter {
	wgc := &WgCounter{}

	wgc.count.Add(uint32(bufferSize))
	wgc.wg.Add(bufferSize)

	return wgc
}

func (pt *WgCounter) Count() int {
	return int(pt.count.Load())
}

// Done counts one job off and reports whether it was the last one.
func (pt *WgCounter) Done() bool {
	if pt.count.Load() == 0 {
		return false
	}

	last := pt.count.Add(^uint32(0)) == 0
	pt.wg.Done()

	return last
}

func (pt *WgCounter) Wait() {
	pt.wg.Wait()
}
