package types

// PQItem represents the interface for a generic common concurrent queue.
type ICQueue interface {
	// IsPaused returns whether the queue is paused.
	IsPaused() bool
	// Restart restarts the queue and initializes the worker goroutines based on the concurrency.
	// Time complexity: O(n) where n is the concurrency
	Restart()
	// Resume continues processing jobs those are pending in the queue.
	Resume()
	// PendingCount returns the number of Jobs pending in the queue.
	// Time complexity: O(1)
	PendingCount() int
	// CurrentProcessingCount returns the number of Jobs currently being processed.
	// Time complexity: O(1)
	CurrentProcessingCount() uint32
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

type IConcurrentQueue[T, R any] interface {
	ICQueue
	// Pause pauses the processing of jobs.
	Pause() IConcurrentQueue[T, R]
	// Add adds a new Job to the queue and returns a EnqueuedJob to handle the job.
	// Time complexity: O(1)
	Add(data T) EnqueuedJob[R]
	// AddAll adds multiple Jobs to the queue and returns a EnqueuedGroupJob to handle the job.
	// Time complexity: O(n) where n is the number of Jobs added
	AddAll(data []T) EnqueuedGroupJob[R]
}

type IConcurrentPriorityQueue[T, R any] interface {
	ICQueue
	// Pause pauses the processing of jobs.
	Pause() IConcurrentPriorityQueue[T, R]
	// Add adds a new Job with the given priority to the queue and returns a channel to receive the result.
	// Time complexity: O(log n)
	Add(data T, priority int) EnqueuedJob[R]
	// AddAll adds multiple Jobs with the given priority to the queue and returns a channel to receive all responses.
	// Time complexity: O(n log n) where n is the number of Jobs added
	AddAll(items []PQItem[T]) EnqueuedGroupJob[R]
}
