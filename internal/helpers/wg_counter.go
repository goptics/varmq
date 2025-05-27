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

func (pt *WgCounter) Done() {
	if pt.count.Load() == 0 {
		return
	}

	pt.count.Add(^uint32(0))
	pt.wg.Done()
}

func (pt *WgCounter) Wait() {
	pt.wg.Wait()
}
