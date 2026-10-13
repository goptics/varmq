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
// reads every message; readers of the same group share its position. Method
// signatures use only built-in types, so adapters need not import varmq.
type IPersistentTopic interface {
	// Publish appends one serialized message for every group.
	Publish(item any) bool
	// JoinGroup creates the group at the current head if it does not exist.
	JoinGroup(group string) error
	// GroupLen returns how many messages the group has not read.
	GroupLen(group string) int
	// GroupValues returns the group's unread messages without consuming them.
	GroupValues(group string) []any
	// DequeueGroup returns the group's next message and an ack ID.
	DequeueGroup(group string) (any, bool, string)
	// AcknowledgeGroup commits a message the group finished processing.
	AcknowledgeGroup(group, ackID string) bool
	// PurgeGroup skips the group's position to the head.
	PurgeGroup(group string)
	// DeleteGroup forgets the group and its position.
	DeleteGroup(group string) error
	// Close releases the storage.
	Close() error
}
