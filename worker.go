package gocq

// WorkerFunc is a worker function that takes an input of type T and returns an output of type R along with an error.
type WorkerFunc[T, R any] func(T) (R, error)

// WorkerErrFunc is a worker function that does not return any value, except an error.
type WorkerErrFunc[T any] func(T) error
