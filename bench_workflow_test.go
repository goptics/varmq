package varmq

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkWorkflowPromotion reports, as ns/op, the time from the end of
// task A's function (just before its job closes) to the start of its
// dependent B, in-memory. Each op submits a fresh A -> B workflow and waits
// for B, so it is the promotion latency, not throughput.
func BenchmarkWorkflowPromotion(b *testing.B) {
	ctx := context.Background()
	store := NewWorkflowStore()

	var endA atomic.Int64 // unix ns when A's function returns
	var total atomic.Int64
	done := make(chan struct{}, 1)

	aQ := NewErrWorker(func(j Job[int]) error {
		endA.Store(time.Now().UnixNano())
		return nil
	}).BindQueue()
	bQ := NewErrWorker(func(j Job[int]) error {
		total.Add(time.Now().UnixNano() - endA.Load())
		done <- struct{}{}
		return nil
	}).BindQueue()

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

	b.ReportMetric(float64(total.Load())/float64(b.N), "ns/op")
}
