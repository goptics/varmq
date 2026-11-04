// Package contract is the Go copy of varmq's spec (spec/openapi.yaml,
// spec/redis-storage.md): the types storage implements and observers read.
// It has no dependencies, so storage such as redisq imports it without
// importing varmq. varmq re-exports every name here.
package contract

import "encoding/json"

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
type QueueStats struct {
	Counts JobCounts `json:"counts"`
	Totals JobTotals `json:"totals"`
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
	QueueStats() (QueueStats, error)
	// Jobs returns up to limit jobs in state, skipping the first offset.
	// Pending jobs come in the order the queue hands them out; completed
	// and failed jobs newest first.
	Jobs(state JobState, offset, limit int) ([]JobRecord, error)
}
