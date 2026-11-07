package queues

import (
	"sync"
	"sync/atomic"
	"time"
)

// pauseState is an in-memory storage's queue pause (varmq.QueuePauser and
// varmq.PauseNotifier): Dequeue hands out nothing while at is set.
type pauseState struct {
	at    atomic.Int64  // Unix ms of the Pause; 0 while running
	epoch atomic.Uint64 // +1 before each Pause or Resume changes at (PauseEpoch)
	mu    sync.Mutex
	hooks map[*func(bool)]struct{}
}

// Pause stops Dequeue. Idempotent; the first pause time is kept.
func (p *pauseState) Pause() error {
	p.epoch.Add(1) // before at: a Dequeue refused for this Pause sees it moved
	if p.at.CompareAndSwap(0, time.Now().UnixMilli()) {
		p.notify()
	}
	return nil
}

// Resume lets Dequeue hand out items again. Idempotent.
func (p *pauseState) Resume() error {
	p.epoch.Add(1) // before at: see Pause
	if p.at.Swap(0) != 0 {
		p.notify()
	}
	return nil
}

// Paused reports whether the queue is paused.
func (p *pauseState) Paused() bool { return p.at.Load() != 0 }

// PauseEpoch moves on at each Pause and Resume, before the state changes:
// a Dequeue that came back empty while the queue runs and the epoch did not
// move was not refused for a pause.
func (p *pauseState) PauseEpoch() uint64 { return p.epoch.Load() }

// PausedAt is the Unix ms of the Pause, 0 while running.
func (p *pauseState) PausedAt() int64 { return p.at.Load() }

// OnPauseChange calls fn after every Pause or Resume that changes the state,
// until cancel.
// ponytail: hooks of workers collected without unbinding are dropped by the
// next Pause or Resume (varmq's hook cancels itself), not on register; one
// small closure per such bind until then.
func (p *pauseState) OnPauseChange(fn func(paused bool)) (cancel func()) {
	h := &fn
	p.mu.Lock()
	if p.hooks == nil {
		p.hooks = map[*func(bool)]struct{}{}
	}
	p.hooks[h] = struct{}{}
	p.mu.Unlock()

	return func() {
		p.mu.Lock()
		delete(p.hooks, h)
		p.mu.Unlock()
	}
}

// notify calls the hooks outside mu, so a hook may cancel or register. It
// passes the state as of now, not the change that called it: with a Pause
// and a Resume racing, the last call any hook gets is the final state.
func (p *pauseState) notify() {
	paused := p.Paused()
	p.mu.Lock()
	fns := make([]func(bool), 0, len(p.hooks))
	for h := range p.hooks {
		fns = append(fns, *h)
	}
	p.mu.Unlock()

	for _, fn := range fns {
		fn(paused)
	}
}
