package varmq

import (
	"slices"
	"sync"
	"sync/atomic"
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
//	gj, ok := topic.Publish(order)
//	gj.Wait() // both workers have processed their copy
type Topic[T any] interface {
	// Publish enqueues an independent copy of data into every open
	// subscription and wakes each subscriber's worker.
	//
	// ok is false only when no subscription accepted the copy: zero
	// subscribers, every subscription closed, or the topic closed. The group
	// job is nil only when the topic is closed.
	//
	// The returned group job tracks every delivered copy: Wait blocks until
	// each subscriber has finished (or closed) its copy and NumPending counts
	// copies not yet finished.
	Publish(data T) (EnqueuedGroupJob, bool)
	// NumSubscribers returns the number of open subscriptions.
	NumSubscribers() int
	// Close unsubscribes every subscription and makes further Publish calls
	// return (nil, false). It is idempotent and always returns nil.
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

func (t *topic[T]) Publish(data T) (EnqueuedGroupJob, bool) {
	if t.closed.Load() {
		return nil, false
	}

	subs := t.snapshot()
	gj := newGroupJob[T](len(subs))
	delivered := 0

	for _, s := range subs {
		if s.addGroupJob(gj, data, "") {
			delivered++
		}
	}

	return gj, delivered > 0
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

// subscription is filled in by WithTopic. See worker_binder.go.
type subscription[T any] struct {
	*queue[T]
	worker *worker[T, iJob[T]]
	t      Topic[T]
	once   sync.Once
}

// Unsubscribe is implemented in Task 3.
func (s *subscription[T]) Unsubscribe() {}
