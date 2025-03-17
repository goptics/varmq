package gocq

// Worker is a worker function that takes an input of type T and returns an output of type R along with an error.
type Worker[T, R any] func(T) (R, error)

// VoidWorker is a worker function that does not return any value, except an error.
type VoidWorker[T any] func(T) error
