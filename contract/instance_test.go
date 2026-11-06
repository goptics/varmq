package contract

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRegistry logs each call as "report:<id>" or "remove:<id>". Reports fail
// while failReports is set; removals fail while failRemoves is set.
type fakeRegistry struct {
	events      chan string
	failReports atomic.Bool
	failRemoves atomic.Bool
}

// newFakeRegistry returns a registry with room for 100 unread calls.
func newFakeRegistry() *fakeRegistry { return &fakeRegistry{events: make(chan string, 100)} }

// ReportInstance logs the report and flags an interval other than 1s.
func (f *fakeRegistry) ReportInstance(_ context.Context, r InstanceRecord) error {
	fail := f.failReports.Load() // before the event, so the test's next flag change cannot reach this call
	if r.IntervalMS != 1000 {
		f.events <- "bad interval"
	}
	f.events <- "report:" + r.ID
	if fail {
		return errors.New("down")
	}
	return nil
}

// RemoveInstance logs the removal.
func (f *fakeRegistry) RemoveInstance(_ context.Context, id string) error {
	fail := f.failRemoves.Load() // before the event, as in ReportInstance
	f.events <- "remove:" + id
	if fail {
		return errors.New("down")
	}
	return nil
}

// Instances is unused by the reporter.
func (f *fakeRegistry) Instances(context.Context) ([]InstanceRecord, error) { return nil, nil }

// next returns the next call, failing the test after 3s (a tick is 1s).
func (f *fakeRegistry) next(t *testing.T) string {
	t.Helper()
	select {
	case e := <-f.events:
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("no call within 3s")
		return ""
	}
}

// expect fails the test unless the next calls are want, in order.
func (f *fakeRegistry) expect(t *testing.T, want ...string) {
	t.Helper()
	for _, w := range want {
		if got := f.next(t); got != w {
			t.Fatalf("got %q, want %q", got, w)
		}
	}
}

// withSource sets the instance source to return *id, for the test only.
func withSource(t *testing.T, id *atomic.Value) {
	old := instanceSource.Load()
	if id == nil {
		instanceSource.Store(nil)
	} else {
		SetInstanceSource(func() InstanceRecord { return InstanceRecord{ID: id.Load().(string)} })
	}
	t.Cleanup(func() { instanceSource.Store(old) })
}

// run starts ReportInstances with a 1s interval; cancel stops it and returns
// its error.
func run(reg InstanceRegistry) (cancel func() error) {
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ReportInstances(ctx, reg, time.Second) }()
	return func() error { stop(); return <-done }
}

// Reports go out at start and on each tick; stopping removes the record.
func TestReportInstances(t *testing.T) {
	var id atomic.Value
	id.Store("pod-a")
	withSource(t, &id)
	reg := newFakeRegistry()

	stop := run(reg)
	reg.expect(t, "report:pod-a", "report:pod-a") // at start, then after a tick
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	reg.expect(t, "remove:pod-a")
}

// A changed Instance ID: the old record goes only after the new one lands,
// and stays queued for removal while reports fail.
func TestReportInstancesIDChange(t *testing.T) {
	var id atomic.Value
	id.Store("a")
	withSource(t, &id)
	reg := newFakeRegistry()

	stop := run(reg)
	reg.expect(t, "report:a")

	reg.failReports.Store(true)
	id.Store("b")
	reg.expect(t, "report:b") // failed: a is kept, not removed

	reg.failReports.Store(false)
	reg.expect(t, "report:b", "remove:a")

	if err := stop(); err != nil {
		t.Fatal(err)
	}
	reg.expect(t, "remove:b")
}

// Two ID changes in a row and failing removals: every old ID stays queued
// until its removal succeeds, and an ID changed back to is not removed.
func TestReportInstancesPendingIDs(t *testing.T) {
	var id atomic.Value
	id.Store("a")
	withSource(t, &id)
	reg := newFakeRegistry()

	stop := run(reg)
	reg.expect(t, "report:a")

	reg.failReports.Store(true)
	id.Store("b")
	reg.expect(t, "report:b") // pending: a

	reg.failReports.Store(false)
	reg.failRemoves.Store(true)
	id.Store("c")
	reg.expect(t, "report:c", "remove:a", "remove:b") // both fail, both kept

	reg.failRemoves.Store(false)
	id.Store("a")                                     // back to a: only b and c are old now
	reg.expect(t, "report:a", "remove:b", "remove:c") // a is not removed

	if err := stop(); err != nil {
		t.Fatal(err)
	}
	reg.expect(t, "remove:a")
}

// An empty Instance ID after a report removes the reported record.
func TestReportInstancesEmptyIDAfterReport(t *testing.T) {
	var id atomic.Value
	id.Store("a")
	withSource(t, &id)
	reg := newFakeRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ReportInstances(ctx, reg, time.Second) }()
	reg.expect(t, "report:a")

	id.Store("")
	reg.expect(t, "remove:a") // the record does not outlive the reporter
	if err := <-done; err == nil {
		t.Fatal("empty Instance ID accepted")
	}
}

// Without varmq in the binary there is nothing to report or remove.
func TestReportInstancesNoSource(t *testing.T) {
	withSource(t, nil)
	reg := newFakeRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ReportInstances(ctx, reg, time.Second); err != nil {
		t.Fatal(err)
	}
	if len(reg.events) != 0 {
		t.Fatalf("reported or removed without a source: %q", <-reg.events)
	}
}

// A bad interval or an empty first ID is an error, and nothing is removed.
func TestReportInstancesBadArgs(t *testing.T) {
	reg := newFakeRegistry()
	for _, d := range []time.Duration{999 * time.Millisecond, 61 * time.Second} {
		if ReportInstances(context.Background(), reg, d) == nil {
			t.Fatalf("interval %v accepted", d)
		}
	}

	var id atomic.Value
	id.Store("")
	withSource(t, &id)
	if ReportInstances(context.Background(), reg, time.Second) == nil {
		t.Fatal("empty Instance ID accepted")
	}
	if len(reg.events) != 0 {
		t.Fatal("removed a record that was never reported")
	}
}
