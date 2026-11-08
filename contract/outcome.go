package contract

import "encoding/json"

// JobOutcome is how a job ended, for a producer that waits on it from any
// process (spec/redis-storage.md, section 6.13).
type JobOutcome struct {
	ID    string   `json:"id"`
	State JobState `json:"state"` // JobCompleted or JobFailed
	// Error is the reason of a failed job: its last errors entry.
	Error string `json:"error,omitempty"`
	// Result is the JSON a result worker left (AcknowledgeResult); absent
	// for other jobs and for a result storage dropped.
	Result json.RawMessage `json:"result,omitempty"`
}

// OutcomeWatcher is optional for shared storage: it tells a waiter when a
// job ends, whichever process ran it.
type OutcomeWatcher interface {
	// WatchOutcome calls fn once, from a storage goroutine, when the job
	// with ID id completes or fails. A job that ended before the call is
	// reported too while its Job Record is kept. After fn the watch ends by
	// itself (storage drops it and any subscription it held); cancel stops
	// it early, and is a no-op after fn. fn is not called after cancel
	// returns, except a call already running. fn must not block.
	WatchOutcome(id string, fn func(JobOutcome)) (cancel func())
}

// ResultAcknowledgeable is optional for acknowledgeable storage: Acknowledge
// that keeps the result worker's JSON with the job's record and outcome.
type ResultAcknowledgeable interface {
	// AcknowledgeResult is Acknowledge plus result, valid JSON.
	AcknowledgeResult(ackID string, result []byte) bool
}
