package types

// Result represents the result of a job, containing the data and any error that occurred.
type Result[T any] struct {
	Data T
	Err  error
}

// EnqueuedJob represents a job that has been enqueued and can wait for a result.
type EnqueuedJob[R any] interface {
	IJob
	// WaitForResult blocks until the job completes and returns the result and any error.
	WaitForResult() (R, error)
}

// EnqueuedVoidJob represents a void job that has been enqueued and can wait for an error.
type EnqueuedVoidJob interface {
	IJob
	// WaitForError blocks until an error is received on the error channel.
	WaitForError() error
}

type EnqueuedGroupJob[T any] interface {
	// Drain discards the job's result and error values asynchronously.
	Drain()
	// Results returns a channel that will receive the results of the group
	Results() chan Result[T]
}

type EnqueuedVoidGroupJob interface {
	// Drain discards the job's error values asynchronously.
	Drain()
	// Errors returns a channel that will receive the errors of the void group
	Errors() <-chan error
}
