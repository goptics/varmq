package contract

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRegistry logs each call as "report:<id>" or "remove:<id>".
type fakeRegistry struct {
	events chan string
}

// newFakeRegistry returns a registry with room for 100 unread calls.
func newFakeRegistry() *fakeRegistry { return &fakeRegistry{events: make(chan string, 100)} }

// ReportInstance logs the report and flags an interval other than 1s.
func (f *fakeRegistry) ReportInstance(_ context.Context, r InstanceRecord) error {
	if r.IntervalMS != 1000 {
		f.events <- "bad interval"
	}
	f.events <- "report:" + r.ID
	return nil
}

// RemoveInstance logs the removal.
func (f *fakeRegistry) RemoveInstance(_ context.Context, id string) error {
	f.events <- "remove:" + id
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

// Reports go out at start and on each tick under the ID read at start, even
// after InstanceID changes; stopping removes that record.
func TestReportInstances(t *testing.T) {
	var id atomic.Value
	id.Store("pod-a")
	withSource(t, &id)
	reg := newFakeRegistry()

	stop := run(reg)
	reg.expect(t, "report:pod-a")
	id.Store("pod-b")
	reg.expect(t, "report:pod-a") // after a tick, still the first ID
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	reg.expect(t, "remove:pod-a")
}

// Without varmq in the binary there is nothing to report or remove.
func TestReportInstancesNoSource(t *testing.T) {
	withSource(t, nil)
	reg := newFakeRegistry()

	if err := ReportInstances(context.Background(), reg, time.Second); err != nil { // returns at once
		t.Fatal(err)
	}
	if len(reg.events) != 0 {
		t.Fatalf("reported or removed without a source: %q", <-reg.events)
	}
}

// A bad interval or an empty ID is an error, and nothing is reported.
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
		t.Fatal("reported or removed with an empty ID")
	}
}
