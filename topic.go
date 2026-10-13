package varmq

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/goptics/varmq/internal/queues"
)

// Topic fans out every published payload to all current subscriptions.
//
// Semantics are those of a fanout exchange, not a log: there is no history,
// no offsets and no replay. A subscriber receives only payloads published
// after it subscribed. Each subscription buffers in its own queue, so a slow
// subscriber never blocks the publisher or other subscribers.
//
// The payload is copied by value into each subscriber's job. If T is a
// pointer, or contains pointers, maps or slices, subscribers share the pointee.
//
// Create a topic with [NewTopic]; subscribe a worker with
// [IWorkerBinder.WithTopic].
//
// Example usage:
//
//	topic := varmq.NewTopic[Order]()
//	varmq.NewWorker(sendEmail).WithTopic(topic)
//	varmq.NewWorker(updateStock, 4).WithTopic(topic)
//
//	topic.Publish(order) // both workers receive their own copy
type Topic[T any] interface {
	// Publish enqueues an independent copy of data into every open
	// subscription and wakes each subscriber's worker.
	//
	// It returns false only when no subscription accepted the copy: zero
	// subscribers, every subscription closed, or the topic closed. To wait
	// for a subscriber to finish its copies, use its Worker's Wait.
	Publish(data T) bool
	// NumSubscribers returns the number of open subscriptions.
	NumSubscribers() int
	// Close unsubscribes every subscription and makes further Publish calls
	// return false. It is idempotent and always returns nil.
	Close() error

	add(s *subscription[T])
	remove(s *subscription[T])
}

type topic[T any] struct {
	mx     sync.RWMutex
	subs   []*subscription[T]
	closed atomic.Bool
}

// NewTopic creates an in-memory topic. Each subscription made through
// [IWorkerBinder.WithTopic] gets its own FIFO queue.
func NewTopic[T any]() Topic[T] {
	return &topic[T]{}
}

func (t *topic[T]) snapshot() []*subscription[T] {
	t.mx.RLock()
	defer t.mx.RUnlock()

	return slices.Clone(t.subs)
}

func (t *topic[T]) add(s *subscription[T]) {
	t.mx.Lock()

	// closed is re-checked under the write lock: Close sets the flag before
	// snapshotting, so a subscription either lands in Close's snapshot or is
	// unsubscribed here. Unsubscribe must run outside the lock because it
	// calls remove.
	if t.closed.Load() {
		t.mx.Unlock()
		s.Unsubscribe()
		return
	}

	t.subs = append(t.subs, s)
	t.mx.Unlock()
}

func (t *topic[T]) remove(s *subscription[T]) {
	t.mx.Lock()
	defer t.mx.Unlock()

	for i, cur := range t.subs {
		if cur == s {
			t.subs = slices.Delete(t.subs, i, i+1)
			return
		}
	}
}

func (t *topic[T]) Publish(data T) bool {
	if t.closed.Load() {
		return false
	}

	delivered := 0

	for _, s := range t.snapshot() {
		if _, ok := s.Add(data); ok {
			delivered++
		}
	}

	return delivered > 0
}

func (t *topic[T]) NumSubscribers() int {
	t.mx.RLock()
	defer t.mx.RUnlock()

	return len(t.subs)
}

func (t *topic[T]) Close() error {
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}

	for _, s := range t.snapshot() {
		s.Unsubscribe()
	}

	return nil
}

// Subscription is one worker's inbox for one [Topic]. It is returned by
// [IWorkerBinder.WithTopic]. It has no Add: the topic is the only producer.
type Subscription[T any] interface {
	IExternalBaseQueue
	// Worker returns the bound worker.
	Worker() Worker
	// Unsubscribe closes the inbox, drops any copies still waiting in it,
	// and detaches it from both the topic and the worker. It is idempotent.
	// Close is equivalent to Unsubscribe.
	Unsubscribe()
}

type subscription[T any] struct {
	*queue[T]
	worker *worker[T, iJob[T]] // concrete worker, needed for queues.UnregisterItem; externalBaseQueue.w is only the Worker interface
	t      Topic[T]
	once   sync.Once
}

func newSubscription[T any](w *worker[T, iJob[T]], t Topic[T]) *subscription[T] {
	q := queues.NewQueue[iJob[T]]()

	return &subscription[T]{
		queue:  newQueue(w, q),
		worker: w,
		t:      t,
	}
}

func (s *subscription[T]) Unsubscribe() {
	s.once.Do(func() {
		// Reject further enqueues. queues.Queue.Close only flips a flag,
		// pending items would still drain, so purge them next.
		s.internalQueue.Close()
		// Close every pending copy; each close decrements its group counter,
		// so any Wait on a published group job completes.
		s.Purge()
		s.t.remove(s)
		s.worker.queues.UnregisterItem(s.internalQueue)
	})
}

// Close detaches the subscription. It is the same as Unsubscribe and exists so
// that the IExternalBaseQueue contract (and `defer sub.Close()`) does the
// expected thing: no copies are delivered or allocated for it afterwards.
func (s *subscription[T]) Close() error {
	s.Unsubscribe()
	return nil
}
