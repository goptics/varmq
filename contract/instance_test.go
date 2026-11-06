package contract

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRegistry logs each call as "report:<id>" or "remove:<id>". Reports fail
// while failReports is set.
type fakeRegistry struct {
	events      chan string
	failReports atomic.Bool
}

func newFakeRegistry() *fakeRegistry { return &fakeRegistry{events: make(chan string, 100)} }

func (f *fakeRegistry) ReportInstance(_ context.Context, r InstanceRecord) error {
	if r.IntervalMS != 1000 {
		f.events <- "bad interval"
	}
	f.events <- "report:" + r.ID
	if f.failReports.Load() {
		return errors.New("down")
	}
	return nil
}

func (f *fakeRegistry) RemoveInstance(_ context.Context, id string) error {
	f.events <- "remove:" + id
	return nil
}

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

func run(reg InstanceRegistry) (cancel func() error) {
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ReportInstances(ctx, reg, time.Second) }()
	return func() error { stop(); return <-done }
}

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
