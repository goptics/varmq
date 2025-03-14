package concurrent_queue

// PQItem represents an item with priority for concurrent priority queues.
// Unlike queue.EnqItem, it doesn't track an index as this is handled
// internally by the concurrent queue implementation.
type PQItem[T any] struct {
	// Value contains the actual data stored in the queue
	Value T
	// Priority determines the item's position in the queue
	Priority int
}
