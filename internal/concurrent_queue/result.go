package concurrent_queue

import "github.com/fahimfaisaal/gocq/internal/job"

type Result[T any] struct {
	Data T
	Err  error
}

type EnqueuedJob[T any] interface {
	job.IJob
	WaitForResult() (T, error)
}

type EnqueuedVoidJob[T any] interface {
	job.IJob
	WaitForError() error
}
