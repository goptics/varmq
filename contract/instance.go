package contract

import (
	"context"
	"errors"
	"slices"
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
// interval must be from 1s to 60s. The returned error is a bad argument, an
// empty Instance ID, or the final remove. Failed reports are not returned:
// observers show the process as lost when its reports stop.
func ReportInstances(ctx context.Context, reg InstanceRegistry, interval time.Duration) error {
	if interval < time.Second || interval > time.Minute {
		return errors.New("contract: instance report interval must be from 1s to 60s")
	}

	t := time.NewTicker(interval)
	defer t.Stop()

	// id is the ID being reported. old holds IDs the process reported before
	// InstanceID changed; each is removed once a report under id succeeds,
	// and kept for retry while a report or its removal fails.
	var id string
	var old []string
	removeAll := func() error {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var errs []error
		for _, x := range slices.Concat(old, []string{id}) {
			if x != "" {
				errs = append(errs, reg.RemoveInstance(rctx, x))
			}
		}
		return errors.Join(errs...)
	}

	for {
		if r, ok := InstanceSnapshot(); ok {
			if r.ID == "" {
				return errors.Join(errors.New("contract: instance reports need a non-empty Instance ID"), removeAll())
			}
			if r.ID != id {
				if id != "" {
					old = append(old, id)
				}
				id = r.ID
				old = slices.DeleteFunc(old, func(x string) bool { return x == id }) // changed back
			}
			r.IntervalMS = interval.Milliseconds()
			// ponytail: no error hook for failed reports; add one when someone needs it
			if reg.ReportInstance(ctx, r) == nil {
				old = slices.DeleteFunc(old, func(x string) bool { return reg.RemoveInstance(ctx, x) == nil })
			}
		}

		select {
		case <-ctx.Done():
			return removeAll()
		case <-t.C:
		}
	}
}
