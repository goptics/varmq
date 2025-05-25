package helpers

import (
	"sync"
	"sync/atomic"
)

// WgCounter keeps track of pending jobs in a group and provides
// synchronization mechanisms to track job completion.
type WgCounter struct {
	count atomic.Uint32  // Counter for tracking pending jobs
	wg    sync.WaitGroup // WaitGroup for synchronizing job completion
}

// NewWgCounter creates a new WgCounter with the given buffer size
func NewWgCounter(bufferSize int) *WgCounter {
	wgc := &WgCounter{}

	wgc.count.Add(uint32(bufferSize))
	wgc.wg.Add(bufferSize)

	return wgc
}

// Count returns the number of pending jobs
func (pt *WgCounter) Count() int {
	return int(pt.count.Load())
}

// Done decrements the pending count and returns true if all jobs are done
func (pt *WgCounter) Done() {
	if pt.count.Load() == 0 {
		return
	}

	pt.count.Add(^uint32(0))
	pt.wg.Done()
}

// Wait blocks until all pending jobs are completed
func (pt *WgCounter) Wait() {
	pt.wg.Wait()
}
