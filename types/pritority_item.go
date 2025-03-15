package types

type PQItem[T any] struct {
	// Value contains the actual data stored in the queue
	Value T
	// Priority determines the item's position in the queue
	Priority int
}
