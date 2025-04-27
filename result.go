package gocmq

// Result represents the result of a job, containing the data and any error that occurred.
type Result[T any] struct {
	JobId string
	Data  T
	Err   error
}

// EnqueuedJob represents a job that has been enqueued and can wait for a result.
type EnqueuedJob[R any] interface {
	Job
	// Drain discards the job's result and error values asynchronously.
	Drain() error
	// Result blocks until the job completes and returns the result and any error.
	Result() (R, error)
}

type EnqueuedGroupJob[T any] interface {
	// Drain discards the job's result and error values asynchronously.
	Drain() error
	// Results returns a channel that will receive the results of the group
	Results() (<-chan Result[T], error)
}

type EnqueuedSingleGroupJob[R any] interface {
	Job
	EnqueuedGroupJob[R]
}
