package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/shared/types"
)

// ICQueue is an interface for concurrent queue operations.
type ICQueue[R any] interface {
	// JobById returns the job with the given id.
	JobById(id string) (types.EnqueuedJob[R], error)
	// GroupsJobById returns the groups job with the given id.
	GroupsJobById(id string) (types.EnqueuedSingleGroupJob[R], error)

	PendingCount() int
	// WaitUntilFinished waits until all pending Jobs in the queue are processed.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitUntilFinished()
	// Purge removes all pending Jobs from the queue.
	// Time complexity: O(n) where n is the number of pending Jobs
	Purge()
	// WaitAndClose waits until all pending Jobs in the queue are processed and then closes the queue.
	// Time complexity: O(n) where n is the number of pending Jobs
	WaitAndClose() error
	// Close closes the queue and resets all internal states.
	// Time complexity: O(n) where n is the number of channels
	Close() error
}
