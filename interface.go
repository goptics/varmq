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

// IDedupQueue is optional for IQueue storage. A persistent or distributed
// queue adds a job that has an id with EnqueueOnce, which stores item unless
// an item with the same id was stored within a window the storage sets, and
// returns true in both cases. A retried Add, or a storage client's re-send of
// a push whose reply was lost, then stores the job once.
type IDedupQueue interface {
	EnqueueOnce(item any, id string) bool
}

// IDedupPriorityQueue is IDedupQueue for IPriorityQueue storage.
type IDedupPriorityQueue interface {
	EnqueueOnce(item any, priority int, id string) bool
}

// enqueue stores item in q, once per id when the job has one and q
// implements IDedupQueue.
func enqueue(q IQueue, item []byte, id string) bool {
	if d, ok := q.(IDedupQueue); ok && id != "" {
		return d.EnqueueOnce(item, id)
	}
	return q.Enqueue(item)
}

// enqueuePriority is enqueue for priority storage (see IDedupPriorityQueue).
func enqueuePriority(q IPriorityQueue, item []byte, priority int, id string) bool {
	if d, ok := q.(IDedupPriorityQueue); ok && id != "" {
		return d.EnqueueOnce(item, priority, id)
	}
	return q.Enqueue(item, priority)
}

// IAcknowledgeable is the root interface of acknowledgeable operations.
type IAcknowledgeable interface {
	// Returns true if the item was successfully acknowledged, false otherwise.
	Acknowledge(ackID string) bool
	// DequeueWithAckId dequeues an item from the queue
	// Returns the item, a boolean indicating if the operation was successful, and the acknowledgment ID.
	DequeueWithAckId() (any, bool, string)
}

// IBatchAcknowledgeable is optional for acknowledgeable storage. A worker
// whose only source implements it reads that source through one pop per
// dispatch, with no length probes in between.
type IBatchAcknowledgeable interface {
	IAcknowledgeable
	// DequeueBatchWithAckIds moves up to min(len(items), len(ackIDs)) items
	// to pending in one atomic step, each under its own ack id, into
	// items[:n] and ackIDs[:n]. remaining is the storage length right after
	// the move. n is 0 with a nil error when the storage is empty. A short
	// pop (n below the count) normally means the storage ran dry; storage may
	// also return a short pop with remaining > 0, for example items it hands
	// back from an earlier failed call. err is a failure: the worker reports
	// it and pulls again on the next sign of work.
	DequeueBatchWithAckIds(items []any, ackIDs []string) (n, remaining int, err error)
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
