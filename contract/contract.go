// Package contract is the Go copy of varmq's spec (spec/openapi.yaml,
// spec/redis-storage.md): the types storage implements and observers read.
// It has no dependencies, so storage such as redisq imports it without
// importing varmq. varmq re-exports every name here.
package contract

import (
	"context"
	"encoding/json"
)

// JobState is where a job is in its life (spec/openapi.yaml, JobState).
type JobState string

const (
	JobPending    JobState = "pending"    // in the queue, not handed out
	JobProcessing JobState = "processing" // handed out, no outcome yet
	JobCompleted  JobState = "completed"  // acknowledged
	JobFailed     JobState = "failed"     // rejected
)

// JobCounts is how many jobs a queue holds in each Job State right now.
// Completed and Failed count the Job Records storage still keeps, which
// retention bounds; JobTotals counts every job.
type JobCounts struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Completed  int64 `json:"completed"`
	Failed     int64 `json:"failed"`
}

// JobTotals counts jobs since the queue's storage began counting.
type JobTotals struct {
	Submitted uint64 `json:"submitted"`
	Completed uint64 `json:"completed"`
	Failed    uint64 `json:"failed"`
}

// QueueStats is a queue's counts and totals, as GET /queues serves them.
// Paused is the durable queue pause: storage hands out no job while it is
// set. PausedAt is when it was set, Unix ms on the storage's clock. A
// QueueInspector whose storage can pause reports both; varmq does not fill
// them in.
type QueueStats struct {
	Counts   JobCounts `json:"counts"`
	Totals   JobTotals `json:"totals"`
	Paused   bool      `json:"paused"`
	PausedAt int64     `json:"paused_at,omitempty"`
}

// JobRecord is one job as GET /queues/{id}/jobs serves it. Times are Unix
// milliseconds, 0 when unknown.
type JobRecord struct {
	ID         string          `json:"id"`
	State      JobState        `json:"state"`
	Payload    json.RawMessage `json:"payload"`
	EnqueuedAt int64           `json:"enqueued_at,omitempty"`
	StartedAt  int64           `json:"started_at,omitempty"`
	FinishedAt int64           `json:"finished_at,omitempty"`
	Attempts   int             `json:"attempts"`
	Errors     []JobError      `json:"errors,omitempty"`
}

// JobError is one failed attempt of a job. A JobRecord keeps at most the last
// 10, oldest first; the last is the job's final error.
type JobError struct {
	Attempt int    `json:"attempt"`
	At      int64  `json:"at"`
	Error   string `json:"error"`
}

// QueueInspector is optional for storage that keeps its own job counts and
// records, such as a shared Redis queue (spec/redis-storage.md). varmq's
// Handler serves GET /queues and GET /queues/{id}/jobs from every bound queue
// that implements it; other queues are left out.
type QueueInspector interface {
	// QueueID is the queue's identity, the same on every instance that
	// binds it, so observers can group instances' answers by it.
	QueueID() string
	// QueueStats returns the queue's counts and totals.
	QueueStats(ctx context.Context) (QueueStats, error)
	// Jobs returns up to limit jobs in state, skipping the first offset.
	// Pending jobs come in the order the queue hands them out; completed
	// and failed jobs newest first.
	Jobs(ctx context.Context, state JobState, offset, limit int) ([]JobRecord, error)
}

// InstanceRecord is what one varmq process reports about itself to shared
// storage (spec/redis-storage.md, section 8). Times are Unix milliseconds.
type InstanceRecord struct {
	// ID is the process's Instance ID, the value of its X-Varmq-Instance
	// header. Two processes in one namespace never share one.
	ID        string `json:"id"`
	Host      string `json:"host"`
	PID       int    `json:"pid"`
	Version   string `json:"version"`
	StartedAt int64  `json:"started_at"`
	// BeatAt is the time of the last report, from the storage clock. Storage
	// never keeps it in the record; a reader fills it in, and a writer leaves
	// it 0.
	BeatAt     int64 `json:"beat_at,omitempty"`
	IntervalMS int64 `json:"interval_ms"`
	// Lost is true when BeatAt is older than 3 × IntervalMS by the storage
	// clock: the process probably stopped without removing its record. A
	// reader sets it; storage never keeps it.
	Lost    bool             `json:"lost,omitempty"`
	Workers []InstanceWorker `json:"workers"`
	// Owners are the IDs of the queue objects this process has open, which
	// prefix the ack IDs of the jobs it holds. Storage fills them in.
	Owners []string `json:"owners,omitempty"`
}

// InstanceWorker is one worker in an InstanceRecord. Its fields match the
// WorkerSummary of GET /workers.
type InstanceWorker struct {
	Name           string `json:"name"`
	Status         string `json:"status"` // a WorkerStatus value
	Concurrency    int    `json:"concurrency"`
	NumProcessing  int    `json:"num_processing"`
	NumIdleWorkers int    `json:"num_idle_workers"`
	// Completed, Successful and Failed count the jobs this worker finished
	// in this process (spec redis-storage.md 8.2, since 1.2.0). A queue's
	// submitted count lives with the queue (QueueStats.Totals), not here.
	Completed  uint64 `json:"completed"`
	Successful uint64 `json:"successful"`
	Failed     uint64 `json:"failed"`
	// QueueIDs are the Queue IDs of the queues the worker is bound to; queues
	// without one, such as in-memory queues, are left out.
	QueueIDs []string `json:"queue_ids"`
}

// InstanceRegistry is optional for shared storage that keeps Instance
// Records (spec/redis-storage.md, section 8), so observers can list live
// processes without calling them.
type InstanceRegistry interface {
	// ReportInstance writes r and marks it seen now.
	ReportInstance(ctx context.Context, r InstanceRecord) error
	// RemoveInstance deletes the record with this Instance ID.
	RemoveInstance(ctx context.Context, id string) error
	// Instances returns every record storage still keeps, with BeatAt and
	// Lost set from the storage clock.
	Instances(ctx context.Context) ([]InstanceRecord, error)
}
