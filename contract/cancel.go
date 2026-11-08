package contract

import "errors"

// ErrJobEnded is CancelJob's answer for a job whose record shows it
// already completed, failed or was cancelled.
var ErrJobEnded = errors.New("varmq: job already ended")

// JobCanceller is optional for shared storage: cancel a job by ID from any
// process (spec/redis-storage.md, section 6.14).
type JobCanceller interface {
	// CancelJob marks job id cancelled. A job still waiting is dropped when
	// a consumer would pop it (outcome cancelled); a job running is
	// announced to the queue's consumers (CancelNotifier), and its worker
	// stops it if it watches its context. ErrJobEnded when its record shows
	// it ended. Each mark expires 24 h after its request. Storage can't tell
	// a waiting job from an unknown ID: authorize and rate-limit untrusted
	// callers. A workflow item (envelope with wf) is never dropped at pop:
	// pop alone can't end its workflow, so it is handed out and its mark
	// stays. Cancel a waiting workflow task with Workflow.Cancel; CancelJob
	// still reaches a running one through CancelNotifier.
	CancelJob(id string) error
}

// CancelNotifier is optional for shared storage: it tells this process of
// cancel requests, so a worker running the job can cancel it. varmq finds
// the running job by storage value and ID, so the storage value must be
// comparable (pointer storage is); other storage gets no watch.
type CancelNotifier interface {
	// OnCancel calls fn with the job ID of each cancel request, until stop.
	// A request whose mark is gone by then (the job finished, or its ID
	// was re-added) is stale: storage SHOULD drop it, not call fn.
	OnCancel(fn func(jobID string)) (stop func())
}

// CancelAcknowledgeable is optional for acknowledgeable storage: finish a
// job that stopped because it was cancelled while it ran. Outcome
// cancelled, never retried.
type CancelAcknowledgeable interface {
	AcknowledgeCancelled(ackID, reason string) bool
}
