package concurrent_queue

import "io"

type IQueue[T, R any] interface {
	io.Closer
	Restart()
	Resume()
	PendingCount() int
	CurrentProcessingCount() uint32
	WaitUntilFinished()
	Purge()
	WaitAndClose() error
}
