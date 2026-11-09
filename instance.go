package varmq

import (
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/goptics/varmq/contract"
)

// processStart is when this process loaded varmq, the InstanceRecord's
// StartedAt.
var processStart = time.Now()

// hostname is read once; it does not change while the process runs.
var hostname, _ = os.Hostname()

// init hands this process's description to the contract module, so storage
// with instance reports turned on (redisq.WithInstanceReports) can write it
// without importing varmq. Nothing is written unless storage turns it on.
func init() { contract.SetInstanceSource(instanceRecord) }

// instanceRecord builds this process's record from WorkerRegistry, the same
// workers GET /workers lists. The reporter fills IntervalMS; storage fills
// Owners, BeatAt and Lost.
func instanceRecord() InstanceRecord {
	r := InstanceRecord{
		ID:        InstanceID,
		Host:      hostname,
		PID:       os.Getpid(),
		Version:   varmqVersion(),
		StartedAt: processStart.UnixMilli(),
		Workers:   []InstanceWorker{},
	}

	WorkerRegistry.Range(func(_, v any) bool {
		w, ok := v.(Worker)
		if !ok {
			return true
		}
		iw := InstanceWorker{
			Name:           w.Name(),
			Status:         w.Status(),
			Concurrency:    w.NumConcurrency(),
			NumProcessing:  w.NumProcessing(),
			NumIdleWorkers: w.NumIdleWorkers(),
			Completed:      w.Metrics().Completed(),
			Successful:     w.Metrics().Successful(),
			Failed:         w.Metrics().Failed(),
			QueueIDs:       []string{},
		}
		if q, ok := v.(interface{ boundQueueIDs() []string }); ok {
			iw.QueueIDs = q.boundQueueIDs()
		}
		r.Workers = append(r.Workers, iw)
		return true
	})
	slices.SortFunc(r.Workers, func(a, b InstanceWorker) int { return strings.Compare(a.Name, b.Name) })

	return r
}

// lifecycleChanged makes instance reporters report now: the worker started,
// stopped, paused, resumed, was tuned or left the registry. Idle/Running
// flips are not lifecycle changes and wait for the next tick. Unnamed
// workers are not in records, so they skip it.
func (w *worker[T, JobType]) lifecycleChanged() {
	if w.Name() != "" {
		contract.InstanceChanged()
	}
}

// boundQueueIDs returns the Queue IDs of the worker's bound QueueInspectors,
// sorted.
func (w *worker[T, JobType]) boundQueueIDs() []string {
	ids := []string{}
	w.queueIDs.Range(func(k, _ any) bool {
		ids = append(ids, k.(string))
		return true
	})
	slices.Sort(ids)
	return ids
}

// listedQueueIDs is boundQueueIDs plus the IDs of the worker's in-memory
// queues (GET /queues lists them while the worker is registered), sorted.
func (w *worker[T, JobType]) listedQueueIDs() []string {
	ids := w.boundQueueIDs()
	w.memQueues.Range(func(k, _ any) bool {
		ids = append(ids, k.(string))
		return true
	})
	slices.Sort(ids)
	return ids
}

// varmqVersion is the varmq module version this binary was built with, or ""
// when unknown (tests, builds without module info).
func varmqVersion() string { return depVersion(debug.ReadBuildInfo()) }

// depVersion is varmq's version in bi, or "" when bi is missing or lacks it.
func depVersion(bi *debug.BuildInfo, ok bool) string {
	if !ok {
		return ""
	}
	for _, d := range bi.Deps {
		if d.Path == "github.com/goptics/varmq" {
			return d.Version
		}
	}
	return ""
}
