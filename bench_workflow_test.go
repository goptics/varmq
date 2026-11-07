package varmq

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkWorkflowPromotion reports, as promote-ns/op, the time from the end
// of task A's function (just before its job closes) to the start of its
// dependent B, in-memory. Each op submits a fresh A -> B workflow and waits
// for B, so it is promotion latency, not throughput.
func BenchmarkWorkflowPromotion(b *testing.B) {
	ctx := context.Background()
	// ponytail: one store per run; a store only sweeps ended workflows by
	// retention, so a long -benchtime holds its b.N records until the run ends.
	store := NewWorkflowStore()

	var endA atomic.Pointer[time.Time] // when A's function returns
	var total atomic.Int64
	done := make(chan struct{}, 1)

	aw := NewErrWorker(func(j Job[int]) error {
		t := time.Now()
		endA.Store(&t)
		return nil
	})
	bw := NewErrWorker(func(j Job[int]) error {
		total.Add(int64(time.Since(*endA.Load())))
		done <- struct{}{}
		return nil
	})
	defer aw.StopAndWait()
	defer bw.StopAndWait()
	aQ, bQ := aw.BindQueue(), bw.BindQueue()

	b.ResetTimer()
	for i := range b.N {
		wf := NewWorkflow(store, WithWorkflowID(fmt.Sprint("bench-", i)))
		a := wf.Add("a", aQ, i)
		wf.Add("b", bQ, i).After(a)
		if err := wf.Submit(ctx); err != nil {
			b.Fatal(err)
		}
		<-done
	}
	b.StopTimer()

	b.ReportMetric(float64(total.Load())/float64(b.N), "promote-ns/op")
}
