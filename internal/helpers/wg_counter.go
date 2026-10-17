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

// Done counts one job off and reports whether it was the last one. The count
// never goes below zero: a Done past the last one is a no-op, however many
// race for it.
func (pt *WgCounter) Done() bool {
	for {
		n := pt.count.Load()
		if n == 0 {
			return false
		}

		if pt.count.CompareAndSwap(n, n-1) {
			pt.wg.Done()
			return n == 1
		}
	}
}

func (pt *WgCounter) Wait() {
	pt.wg.Wait()
}
