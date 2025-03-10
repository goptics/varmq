package queue

type Job[T, R any] struct {
	Data     T
	Response chan R
	Err      chan error
}
