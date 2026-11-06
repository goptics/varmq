package contract

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// instanceSource describes this process. varmq sets it in init, so storage
// can report the process without importing varmq.
var instanceSource atomic.Pointer[func() InstanceRecord]

// SetInstanceSource sets the function InstanceSnapshot calls. varmq calls it
// once, in init; applications do not.
func SetInstanceSource(f func() InstanceRecord) { instanceSource.Store(&f) }

// InstanceSnapshot describes this process now, or reports false when nothing
// set a source (varmq is not in the binary). IntervalMS, BeatAt, Lost and
// Owners are left for the reporter and storage to fill.
func InstanceSnapshot() (InstanceRecord, bool) {
	f := instanceSource.Load()
	if f == nil {
		return InstanceRecord{}, false
	}
	return (*f)(), true
}

// ReportInstances writes InstanceSnapshot to reg at start and then every
// interval (spec/redis-storage.md, section 8). It blocks until ctx ends, then
// removes the record. Storage runs it for an option such as redisq's
// WithInstanceReports; applications do not call it.
//
// The Instance ID is read once, at start: set varmq.InstanceID before
// building storage that reports. A later change is not followed. With no
// source (varmq not in the binary) it returns nil at once.
//
// interval must be from 1s to 60s. The returned error is a bad argument, an
// empty Instance ID, or the final remove. Failed reports are not returned:
// observers show the process as lost when its reports stop.
func ReportInstances(ctx context.Context, reg InstanceRegistry, interval time.Duration) error {
	if interval < time.Second || interval > time.Minute {
		return errors.New("contract: instance report interval must be from 1s to 60s")
	}
	first, ok := InstanceSnapshot()
	if !ok {
		return nil
	}
	id := first.ID
	if id == "" {
		return errors.New("contract: instance reports need a non-empty Instance ID")
	}

	t := time.NewTicker(interval)
	defer t.Stop()

	for r := first; ; r, _ = InstanceSnapshot() {
		r.ID = id // ponytail: fixed at start, so one record per run; follow changes if someone needs it
		r.IntervalMS = interval.Milliseconds()
		_ = reg.ReportInstance(ctx, r) // ponytail: no error hook; add one when someone needs it

		select {
		case <-ctx.Done():
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			return reg.RemoveInstance(rctx, id)
		case <-t.C:
		}
	}
}
