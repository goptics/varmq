package types

type IJob interface {
	ID() string
	// IsClosed returns whether the job is closed.
	IsClosed() bool
	// Status returns the current status of the job.
	Status() string
	// Close closes the job and its associated channels.
	Close() error
}
