package contract

import "encoding/json"

// JobOutcome is how a job ended, for a producer that waits on it from any
// process (spec/redis-storage.md, section 6.13).
type JobOutcome struct {
	ID    string   `json:"id"`
	State JobState `json:"state"` // JobCompleted, JobFailed or JobCancelled
	// Error is the reason of a failed or cancelled job.
	Error string `json:"error,omitempty"`
	// Result is the JSON a result worker left (AcknowledgeResult); absent
	// for other jobs and for a result storage dropped.
	Result json.RawMessage `json:"result,omitempty"`
}

// OutcomeWatcher is optional for shared storage: it tells a waiter when a
// job ends, whichever process ran it. An EnqueueOnce that stores its id
// MUST drop that id's ended record in the same step (spec 6.13 Re-add):
// varmq catches up on a given id after the push (see OutcomeSubscriber),
// and a kept record would report the earlier run as the new job's outcome.
type OutcomeWatcher interface {
	// WatchOutcome calls fn once, from a storage goroutine, when the job
	// with ID id completes, fails or is cancelled. A job that ended before the call is
	// reported too while its Job Record is kept. After fn the watch ends by
	// itself (storage drops it and any subscription it held); cancel stops
	// it early, and is a no-op after fn. fn is not called after cancel
	// returns, except a call already running. fn must not block.
	WatchOutcome(id string, fn func(JobOutcome)) (cancel func())
}

// OutcomeSubscriber is optional for an OutcomeWatcher: WatchOutcome in two
// steps. varmq subscribes to a given ID before its EnqueueOnce and catches
// up after it, so an outcome that comes before the push returns is not
// lost on storage that keeps no record of it, and the catch-up never sees
// the record of the ID's earlier run (the push drops it, spec 6.13).
// Without it varmq watches a given ID after the push.
type OutcomeSubscriber interface {
	OutcomeWatcher
	// SubscribeOutcome is WatchOutcome without the catch-up: it returns once
	// fn gets every outcome of id that ends from then on. catchUp reports a
	// job that ended before it while its Job Record is kept, as WatchOutcome
	// does; call it at most once. cancel is WatchOutcome's.
	SubscribeOutcome(id string, fn func(JobOutcome)) (catchUp, cancel func())
}

// ResultAcknowledgeable is optional for acknowledgeable storage: Acknowledge
// that keeps the result worker's JSON with the job's record and outcome.
type ResultAcknowledgeable interface {
	// AcknowledgeResult is Acknowledge plus result, valid JSON.
	AcknowledgeResult(ackID string, result []byte) bool
}
