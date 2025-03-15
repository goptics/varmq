package types

type IJob interface {
	// IsLocked returns whether the job is closed.
	IsClosed() bool
	// Status returns the current status of the job.
	Status() string
	// Drain discards the job's result and error values asynchronously.
	Drain()
	// Close closes the job and its associated channels.
	Close() error
}
