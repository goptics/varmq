package helpers

import (
	"sync/atomic"
)

// WgCounter counts a group's jobs down; done closes when the last ends.
type WgCounter struct {
	count atomic.Uint32
	done  chan struct{}
}

func NewWgCounter(bufferSize int) *WgCounter {
	wgc := &WgCounter{done: make(chan struct{})}

	wgc.count.Add(uint32(bufferSize))
	if bufferSize == 0 {
		close(wgc.done)
	}

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
			if n == 1 {
				close(pt.done)
			}
			return n == 1
		}
	}
}

func (pt *WgCounter) Wait() {
	<-pt.done
}

// Ended is closed once every job ended, for a select with a deadline.
func (pt *WgCounter) Ended() <-chan struct{} {
	return pt.done
}
