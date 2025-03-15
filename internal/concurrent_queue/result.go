package concurrent_queue

import (
	"github.com/fahimfaisaal/gocq/internal/job"
	"github.com/fahimfaisaal/gocq/internal/shared"
)

// Result represents the result of a job, containing the data and any error that occurred.
// EnqueuedJob represents a job that has been enqueued and can wait for a result.
type EnqueuedJob[T any] interface {
	job.IJob
	WaitForResult() (T, error)
}

// EnqueuedVoidJob represents a void job that has been enqueued and can wait for an error.
type EnqueuedVoidJob interface {
	job.IJob
	WaitForError() error
}

type EnqueuedGroupJob[T any] interface {
	Drain()
	Result() chan shared.Result[T]
}

type EnqueuedVoidGroupJob interface {
	EnqueuedGroupJob[any]
	Result() chan shared.Result[any]
}
