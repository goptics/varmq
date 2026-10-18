package varmq

import (
	"cmp"
	"slices"
	"sync/atomic"

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
func (w *worker[T, JobType]) park(jobs []JobType, stamp pool.Stamp) {
	w.parkMx.Lock()
	defer w.parkMx.Unlock()

	if stamp.Stale() {
		for _, j := range jobs {
			_ = j.Close()
		}
		return
	}

	i, _ := slices.BinarySearchFunc(w.parked, stamp.Seq, func(b parkedBatch[JobType], seq uint64) int {
		return cmp.Compare(b.stamp.Seq, seq)
	})
	w.parked = slices.Insert(w.parked, i, parkedBatch[JobType]{jobs: slices.Clone(jobs), stamp: stamp})
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

	b := &w.parked[0]
	n := copy(buf, b.jobs)
	stamp := b.stamp

	clear(b.jobs[:n])
	b.jobs = b.jobs[n:]
	b.stamp.Seq += uint64(n)
	if len(b.jobs) == 0 {
		w.parked = slices.Delete(w.parked, 0, 1)
	}
	w.nParked.Add(-int64(n))

	return n, stamp
}

// dropParked closes the parked jobs taken from the source whose purge
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
