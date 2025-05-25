package varmq

// Result represents the result of a job, containing the data and any error that occurred.
type Result[T any] struct {
	JobId string
	Data  T
	Err   error
}

type Identifiable interface {
	ID() string
}

type StatusProvider interface {
	IsClosed() bool
	Status() string
}

type Awaitable interface {
	// Wait blocks until the job is closed.
	Wait()
}

type Drainer interface {
	Drain() error
}
