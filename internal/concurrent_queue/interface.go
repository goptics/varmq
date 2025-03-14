package concurrent_queue

import "io"

// ICQueue represents the interface for a generic common concurrent queue.
type ICQueue[T, R any] interface {
	io.Closer
	Restart()
	Resume()
	PendingCount() int
	CurrentProcessingCount() uint32
	WaitUntilFinished()
	Purge()
	WaitAndClose() error
}
