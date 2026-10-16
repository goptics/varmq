package varmq

// IBaseQueue is the root interface of queue operations. workers queue needs to implement this interface.
type IBaseQueue interface {
	Len() int
	Dequeue() (any, bool)
	Values() []any
	Purge()
	Close() error
}

// IQueue is the root interface of queue operations.
type IQueue interface {
	IBaseQueue
	Enqueue(item any) bool
}

// IPriorityQueue is the root interface of priority queue operations.
type IPriorityQueue interface {
	IBaseQueue
	Enqueue(item any, priority int) bool
}

// IAcknowledgeable is the root interface of acknowledgeable operations.
type IAcknowledgeable interface {
	// Returns true if the item was successfully acknowledged, false otherwise.
	Acknowledge(ackID string) bool
	// DequeueWithAckId dequeues an item from the queue
	// Returns the item, a boolean indicating if the operation was successful, and the acknowledgment ID.
	DequeueWithAckId() (any, bool, string)
}

type IPersistentQueue interface {
	IQueue
	IAcknowledgeable
}

// IPersistentPriorityQueue is the root interface of persistent priority queue operations.
type IPersistentPriorityQueue interface {
	IPriorityQueue
	IAcknowledgeable
}

// ISubscribable is the root interface of subscribable operations.
type ISubscribable interface {
	Subscribe(func(action string))
}

// IDistributedQueue is the root interface of distributed queue operations.
type IDistributedQueue interface {
	IPersistentQueue
	ISubscribable
}

// IDistributedPriorityQueue is the root interface of distributed priority queue operations.
type IDistributedPriorityQueue interface {
	IPersistentPriorityQueue
	ISubscribable
}

// CapacitySetter is the interface for setting the capacity of a queue. (Optional)
type CapacitySetter interface {
	SetCapacity(cap int)
}

// IPersistentTopic is durable topic storage with named groups. Every group
// reads every message published after it first joined; readers of the same
// group share its messages. Method signatures use only built-in types, so
// adapters need not import varmq.
//
// Delivery is at least once, and that depends on the adapter:
//   - A message returned by DequeueGroup stays pending for that group until
//     AcknowledgeGroup is called with its ack ID. Pending messages that are
//     never acknowledged must be returned again by DequeueGroup, after a
//     restart at the latest.
//   - Acknowledgements can arrive out of order (several workers or a
//     concurrency above 1 share a group). Acknowledging one message must not
//     acknowledge earlier ones.
//   - GroupLen must count only messages DequeueGroup can return right now.
//     varmq's event loop keeps trying while GroupLen is above 0, and calls it
//     several times per message, so it should be cheap.
//   - DequeueGroup must return exactly the value given to Publish (varmq
//     publishes []byte).
//
// varmq may still call AcknowledgeGroup for in-flight messages after Close or
// DeleteGroup; return false then.
type IPersistentTopic interface {
	// Publish appends one serialized message for every existing group.
	Publish(item any) bool
	// JoinGroup creates the group at the current head if it does not exist.
	// It is idempotent.
	JoinGroup(group string) error
	// GroupLen returns how many messages DequeueGroup can return now.
	GroupLen(group string) int
	// DequeueGroup returns the group's next message and its ack ID, and marks
	// it pending.
	DequeueGroup(group string) (any, bool, string)
	// AcknowledgeGroup completes one pending message. It returns false for an
	// unknown group or ack ID.
	AcknowledgeGroup(group, ackID string) bool
	// PurgeGroup skips every message the group has not dequeued yet.
	PurgeGroup(group string)
	// DeleteGroup forgets the group, its position and its pending messages.
	// Deleting a missing group returns nil.
	DeleteGroup(group string) error
	// Close releases the storage.
	Close() error
}
