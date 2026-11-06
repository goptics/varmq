package contract

import (
	"context"
	"testing"
	"time"
)

// fakeRegistry records what ReportInstances sends.
type fakeRegistry struct {
	reports chan InstanceRecord
	removed chan string
}

func (f *fakeRegistry) ReportInstance(_ context.Context, r InstanceRecord) error {
	select { // a later tick may report again; keep only the first
	case f.reports <- r:
	default:
	}
	return nil
}

func (f *fakeRegistry) RemoveInstance(_ context.Context, id string) error {
	f.removed <- id
	return nil
}

func (f *fakeRegistry) Instances(context.Context) ([]InstanceRecord, error) { return nil, nil }

func withSource(t *testing.T, f func() InstanceRecord) {
	old := instanceSource.Load()
	if f == nil {
		instanceSource.Store(nil)
	} else {
		SetInstanceSource(f)
	}
	t.Cleanup(func() { instanceSource.Store(old) })
}

func TestReportInstances(t *testing.T) {
	withSource(t, func() InstanceRecord { return InstanceRecord{ID: "pod-a", Workers: []InstanceWorker{}} })

	reg := &fakeRegistry{reports: make(chan InstanceRecord, 1), removed: make(chan string, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ReportInstances(ctx, reg, time.Second) }()

	r := <-reg.reports // the first report is sent at start
	if r.ID != "pod-a" || r.IntervalMS != 1000 {
		t.Fatalf("report = %+v", r)
	}

	cancel()
	if id := <-reg.removed; id != "pod-a" {
		t.Fatalf("removed %q", id)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestReportInstancesNoSource(t *testing.T) {
	withSource(t, nil)

	reg := &fakeRegistry{reports: make(chan InstanceRecord, 1), removed: make(chan string, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ReportInstances(ctx, reg, time.Second); err != nil {
		t.Fatal(err)
	}
	if len(reg.reports) != 0 || len(reg.removed) != 0 {
		t.Fatal("reported or removed without a source")
	}
}

func TestReportInstancesBadArgs(t *testing.T) {
	reg := &fakeRegistry{reports: make(chan InstanceRecord, 1), removed: make(chan string, 1)}
	for _, d := range []time.Duration{999 * time.Millisecond, 61 * time.Second} {
		if ReportInstances(context.Background(), reg, d) == nil {
			t.Fatalf("interval %v accepted", d)
		}
	}

	withSource(t, func() InstanceRecord { return InstanceRecord{} })
	if ReportInstances(context.Background(), reg, time.Second) == nil {
		t.Fatal("empty Instance ID accepted")
	}
}
