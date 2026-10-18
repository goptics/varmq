package varmq

import (
	"cmp"
	"slices"
	"sync"
	"sync/atomic"
	"weak"

	"github.com/goptics/varmq/internal/pool"
)

// parkedBatch is the unstarted rest of a batch a worker held when it
// stopped or paused. stamp.Seq is the dispatch number of jobs[0].
type parkedBatch[JobType any] struct {
	jobs  []JobType
	stamp pool.Stamp
}

// pending is what the worker has not handed out yet: the jobs in its sources
// and the jobs parked by a stop or pause.
func (w *worker[T, JobType]) pending(m sourceManager[JobType]) int {
	return m.Len() + int(w.nParked.Load())
}

// park keeps jobs a worker took in a batch but did not start because the
// worker is stopping or pausing. They stay Queued and count as pending, and
// the event loop hands them out before anything in the sources once the
// worker runs again, in dispatch order across all workers. A batch a Purge
// reached first is closed instead: the check runs under parkMx, so a Purge
// either sees the batch parked or the batch sees the Purge.
//
// Batches are appended as workers park them and sorted once, by the first
// unpark after, so a stop of many workers costs O(n log n), not O(n²).
func (w *worker[T, JobType]) park(jobs []JobType, stamp pool.Stamp) {
	w.parkMx.Lock()
	defer w.parkMx.Unlock()

	if stamp.Stale() {
		for _, j := range jobs {
			_ = j.Close()
		}
		return
	}

	w.parked = append(w.parked, parkedBatch[JobType]{jobs: slices.Clone(jobs), stamp: stamp})
	w.parkUnsorted = true
	w.nParked.Add(int64(len(jobs)))
}

// unpark moves up to len(buf) parked jobs into buf, the oldest first, and
// returns how many and the stamp they carry. 0 when nothing is parked.
func (w *worker[T, JobType]) unpark(buf []JobType) (int, pool.Stamp) {
	w.parkMx.Lock()
	defer w.parkMx.Unlock()

	if len(w.parked) == 0 {
		return 0, pool.Stamp{}
	}

	if w.parkUnsorted {
		slices.SortFunc(w.parked, func(a, b parkedBatch[JobType]) int {
			return cmp.Compare(a.stamp.Seq, b.stamp.Seq)
		})
		w.parkUnsorted = false
	}

	b := &w.parked[0]
	n := copy(buf, b.jobs)
	stamp := b.stamp

	clear(b.jobs[:n])
	b.jobs = b.jobs[n:]
	b.stamp.Seq += uint64(n)
	if len(b.jobs) == 0 {
		// Off the front without shifting the rest: draining stays linear.
		w.parked[0] = parkedBatch[JobType]{}
		w.parked = w.parked[1:]
		if len(w.parked) == 0 {
			w.parked = nil
		}
	}
	w.nParked.Add(-int64(n))

	return n, stamp
}

// dropParked closes the parked jobs taken from the storage whose purge
// counter is gen. Purge calls it after bumping the counter, so a paused or
// stopped worker does not keep purged jobs, and their Wait, until it runs.
func (w *worker[T, JobType]) dropParked(gen *atomic.Uint64) {
	w.parkMx.Lock()
	dropped := 0
	w.parked = slices.DeleteFunc(w.parked, func(b parkedBatch[JobType]) bool {
		if b.stamp.Gen != gen {
			return false
		}
		for _, j := range b.jobs {
			_ = j.Close()
		}
		dropped += len(b.jobs)
		return true
	})
	w.nParked.Add(-int64(dropped))
	w.parkMx.Unlock()

	if dropped > 0 {
		// Fewer pending jobs: Wait and WaitUntilIdle may be done now.
		w.mx.Lock()
		w.waiters.Broadcast()
		w.mx.Unlock()
	}
}

// parkHook is how a Purge reaches a worker's parked jobs. The worker holds
// it; the storage's purgeState holds it only weakly, so binding a queue never
// keeps a discarded worker alive.
type parkHook struct {
	drop func(gen *atomic.Uint64)
}

func (w *worker[T, JobType]) parkHook() *parkHook {
	w.hookOnce.Do(func() { w.hook = &parkHook{drop: w.dropParked} })

	return w.hook
}

// purgeState is what Purge drives for one storage: the counter every batch
// taken from it is stamped with, and the workers that may hold its jobs. A
// queue bound to several workers shares one, so a Purge through any of its
// handles reaches the jobs every one of them took.
type purgeState struct {
	gen   atomic.Uint64
	mx    sync.Mutex
	hooks []weak.Pointer[parkHook]
	// Workers load gen before every job. Padded to 128 bytes, a size class
	// whose objects start on a cache-line boundary, so gen never shares a line
	// with another object that is written per job (the queue's own struct did,
	// next to its cursor, and cost a single producer 15%).
	_ [128 - 8 - 8 - 24]byte
}

// purgeStateFor returns the state shared by everyone bound to q. Storage
// that cannot carry it (custom WithQueue storage) gets one per source, so a
// Purge through one handle reaches only that handle's worker.
func purgeStateFor(q IBaseQueue) *purgeState {
	if a, ok := q.(interface{ Attach(func() any) any }); ok {
		if p, ok := a.Attach(func() any { return &purgeState{} }).(*purgeState); ok {
			return p
		}
	}

	return &purgeState{}
}

// purgeStateOf returns the state a Queue handle's Purge drives for src, or
// nil when src never batches.
func purgeStateOf(src any) *purgeState {
	if s, ok := src.(interface{ purgeStateOf() *purgeState }); ok {
		return s.purgeStateOf()
	}

	return nil
}

func (p *purgeState) register(h *parkHook) {
	p.mx.Lock()
	defer p.mx.Unlock()

	for _, wp := range p.hooks {
		if wp.Value() == h {
			return
		}
	}
	p.hooks = append(p.hooks, weak.Make(h))
}

// purge stales every batch taken so far, then closes the parked jobs of
// every worker still alive, forgetting the ones collected.
func (p *purgeState) purge() {
	p.gen.Add(1)

	p.mx.Lock()
	var live []*parkHook
	kept := p.hooks[:0]
	for _, wp := range p.hooks {
		if h := wp.Value(); h != nil {
			live = append(live, h)
			kept = append(kept, wp)
		}
	}
	clear(p.hooks[len(kept):])
	p.hooks = kept
	p.mx.Unlock()

	for _, h := range live {
		h.drop(&p.gen)
	}
}
