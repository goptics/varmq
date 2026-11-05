package queues

import (
	"sync"
	"sync/atomic"
)

type enqItem[T any] struct {
	Value    T
	Priority int
	Index    int
}

// PriorityQueue is the user-facing wrapper around heapQueue[T].
type PriorityQueue[T any] struct {
	internal       *heapQueue[T]
	insertionCount int
	capacity       int                 // Maximum size of the queue
	ids            map[string]struct{} // ids of stored items EnqueueOnce added; nil until the first
	mx             sync.RWMutex
	closed         atomic.Bool
	idsPeak        uint32 // most ids held since ids was made; under mx
}

// newPriorityQueue initializes an empty priority queue.
func NewPriorityQueue[T any]() *PriorityQueue[T] {
	return &PriorityQueue[T]{internal: &heapQueue[T]{}}
}

func (q *PriorityQueue[T]) SetCapacity(cap int) {
	q.capacity = max(0, cap)
}

// Len returns the number of items in the priority queue.
func (q *PriorityQueue[T]) Len() int {
	q.mx.RLock()
	defer q.mx.RUnlock()
	return q.internal.Len()
}

// Values returns a slice of all values in the priority queue.
func (q *PriorityQueue[T]) Values() []any {
	q.mx.RLock()
	defer q.mx.RUnlock()
	return q.values()
}

// values copies the stored values. Callers hold mx.
func (q *PriorityQueue[T]) values() []any {
	values := make([]any, 0, len(q.internal.items))
	for i := range q.internal.items {
		values = append(values, q.internal.items[i].Value)
	}
	return values
}

// Enqueue pushes a new item with the given priority.
// Time complexity: O(log n)
func (q *PriorityQueue[T]) Enqueue(item any, priority int) bool {
	return q.enqueue(item, priority, "")
}

// EnqueueOnce pushes item, which must return id from its ID method, unless
// an item with the same id is in the queue. It returns false for a repeat and
// where Enqueue would. The id is free again once Dequeue or Purge takes the
// item.
func (q *PriorityQueue[T]) EnqueueOnce(item any, priority int, id string) bool {
	return q.enqueue(item, priority, id)
}

// enqueue pushes item, once per id when id is not empty.
func (q *PriorityQueue[T]) enqueue(item any, priority int, id string) bool {
	typedValue, ok := item.(T)

	if !ok {
		return false
	}

	q.mx.Lock()

	// closed is checked under the lock so a Close followed by Purge cannot
	// interleave with an in-flight Enqueue (see Subscription.Unsubscribe).
	if q.closed.Load() {
		q.mx.Unlock()
		return false
	}

	if q.capacity > 0 && q.internal.Len() == q.capacity {
		q.mx.Unlock()
		return false
	}

	if id != "" {
		if _, dup := q.ids[id]; dup {
			q.mx.Unlock()
			return false
		}

		if q.ids == nil {
			q.ids = make(map[string]struct{})
		}
		q.ids[id] = struct{}{}
		q.idsPeak = max(q.idsPeak, uint32(len(q.ids)))
	}

	q.internal.push(enqItem[T]{
		Value:    typedValue,
		Priority: priority,
		Index:    q.insertionCount,
	}) // O(log n)
	q.insertionCount++
	q.mx.Unlock()

	return true
}

// Dequeue removes and returns the item with the *smallest* Priority.
// Time complexity: O(log n)
func (q *PriorityQueue[T]) Dequeue() (any, bool) {
	q.mx.Lock()
	defer q.mx.Unlock()
	var zeroValue T
	if q.internal.Len() == 0 {
		return zeroValue, false
	}

	v := q.internal.pop().Value // O(log n)

	if len(q.ids) != 0 {
		if j, ok := any(v).(identified); ok {
			if id := j.ID(); id != "" {
				delete(q.ids, id)
				if len(q.ids) == 0 && q.idsPeak > idsShrinkPeak {
					q.ids, q.idsPeak = nil, 0
				}
			}
		}
	}

	return v, true
}

// Purge removes every item and frees their ids. It exists for IBaseQueue,
// whose Purge returns nothing; varmq purges through PurgeValues
// (IPurgeValuesQueue).
func (q *PriorityQueue[T]) Purge() {
	q.PurgeValues()
}

// PurgeValues removes every item, frees their ids and returns the removed
// items, in heap order. It is one step under mx, so an Enqueue racing it is
// either returned or stays queued, never removed unseen.
func (q *PriorityQueue[T]) PurgeValues() []any {
	q.mx.Lock()
	defer q.mx.Unlock()
	values := q.values()
	// Drop the backing array rather than reslicing to 0: it holds the purged
	// values and, at 32 B a slot, would pin the queue's high-water memory.
	q.internal.items = nil
	q.ids, q.idsPeak = nil, 0
	return values
}

// to satisfy the IQueue interface
func (q *PriorityQueue[T]) Close() error {
	q.closed.Store(true)
	return nil
}
