package varmq

import (
	"fmt"
	"sync"
)

// Waiter provides methods to wait for specific worker statuses.
// It uses condition variables for efficient waiting with proper
// spurious wakeup handling through predicate loops.
type Waiter struct {
	cond      *sync.Cond
	getStatus func() status
}

// newWaiter creates a new Waiter instance with the given status checker function.
func newWaiter(mx *sync.RWMutex, getStatus func() status) *Waiter {
	return &Waiter{
		cond:      sync.NewCond(mx),
		getStatus: getStatus,
	}
}

// Wait blocks until the worker reaches Paused, Stopped, or Idle status.
// This is useful when you want to wait for the worker to stop processing
// but don't care which specific state it reaches.
func (wt *Waiter) Wait() {
	wt.cond.L.Lock()
	defer wt.cond.L.Unlock()

	for {
		s := wt.getStatus()
		if s == paused || s == stopped || s == idle {
			fmt.Println("Releasing...", s)
			return
		}
		wt.cond.Wait()
	}
}

// WaitUntilIdle blocks until the worker reaches Idle status specifically.
// The worker must be in idle state (not running, paused, or stopped).
func (wt *Waiter) WaitUntilIdle() {
	wt.cond.L.Lock()
	defer wt.cond.L.Unlock()

	for wt.getStatus() != idle {
		wt.cond.Wait()
	}
}

// WaitUntilPaused blocks until the worker reaches Paused status specifically.
// The worker must be in paused state.
func (wt *Waiter) WaitUntilPaused() {
	wt.cond.L.Lock()
	defer wt.cond.L.Unlock()

	for wt.getStatus() != paused {
		wt.cond.Wait()
	}
}

// WaitUntilStopped blocks until the worker reaches Stopped status specifically.
// The worker must be in stopped state.
func (wt *Waiter) WaitUntilStopped() {
	wt.cond.L.Lock()
	defer wt.cond.L.Unlock()

	for wt.getStatus() != stopped {
		wt.cond.Wait()
	}
}
