package varmq

import (
	"slices"
	"sync"

	"github.com/goptics/varmq/internal/topics"
)

// Topic fans out every published payload to all current subscriptions.
//
// Semantics are those of a fanout exchange, not a replayable log: a
// subscriber receives only payloads published after it subscribed.
//
// Payloads are stored once in a shared in-memory log built from linked
// chunks. Each subscription reads at its own position, so a slow subscriber
// never blocks the publisher or other subscribers. A chunk is freed once the
// slowest subscriber has moved past it, so a stalled or paused subscriber
// holds every chunk published since it stalled until it catches up or
// unsubscribes.
//
// Each subscriber's worker gets its own job wrapping the payload. The payload
// is copied by value; if T is a pointer, or contains pointers, maps or
// slices, subscribers share the pointee. Job IDs are generated when a
// subscriber's worker reads the payload, using that worker's ID generator.
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
//	topic.Publish(order) // both workers get their own job from one shared log
type Topic[T any] interface {
	// Publish appends data to the topic and wakes every subscriber's worker.
	//
	// It returns false, and no subscriber gets the payload, when there are
	// zero subscriptions, the topic is closed, or the topic is full: its
	// slowest subscription already has WithTopicCapacity unread payloads. To
	// wait for a subscriber to finish its payloads, use its Worker's Wait.
	Publish(data T) bool
	// IsFull reports whether the topic has reached its WithTopicCapacity: its
	// slowest subscription already has that many unread payloads, so Publish
	// would return false. It is always false without a capacity. The answer
	// can change right after the call; Publish's result is authoritative.
	IsFull() bool
	// NumSubscribers returns the number of subscriptions attached in this
	// process.
	NumSubscribers() int
	// Close unsubscribes every subscription and makes further Publish calls
	// return false. It is idempotent and always returns nil.
	Close() error

	subscribe(w *worker[T, iJob[T]], priority int) *subscription[T]
}

type topic[T any] struct {
	// mx guards subs and closed, and is held for writing whenever a cursor is
	// created or closed, so the log's open cursors are always exactly subs.
	mx       sync.RWMutex
	log      *topics.Log[T]
	capacity int // WithTopicCapacity; also each subscription's IsFull limit
	subs     []*subscription[T]
	closed   bool
}

// NewTopic creates an in-memory topic. See [Topic] for its semantics.
// configs takes topic options such as WithTopicCapacity.
func NewTopic[T any](configs ...TopicConfigFunc) Topic[T] {
	capacity := loadTopicConfigs(configs...).capacity
	return &topic[T]{log: topics.NewBoundedLog[T](capacity), capacity: capacity}
}

func (t *topic[T]) Publish(data T) bool {
	t.mx.RLock()
	defer t.mx.RUnlock()

	if t.closed {
		return false
	}

	if t.log.Append(data) == 0 {
		return false
	}

	for _, s := range t.subs {
		s.worker.Metrics().incSubmitted()
		s.worker.notifyToPullNextJobs()
	}

	return true
}

func (t *topic[T]) IsFull() bool {
	return t.log.Full()
}

func (t *topic[T]) NumSubscribers() int {
	t.mx.RLock()
	defer t.mx.RUnlock()

	return len(t.subs)
}

func (t *topic[T]) Close() error {
	t.mx.Lock()
	if t.closed {
		t.mx.Unlock()
		return nil
	}

	t.closed = true
	subs := slices.Clone(t.subs)
	t.mx.Unlock()

	// Unsubscribe takes t.mx itself, so it runs after the unlock.
	for _, s := range subs {
		s.Unsubscribe()
	}

	return nil
}

// subscribe attaches a cursor for w. Source is registered under t.mx so
// no payload lands in a cursor its worker can't see. Lock order: t.mx, then
// w.sourcesMx or the log's mx.
func (t *topic[T]) subscribe(w *worker[T, iJob[T]], priority int) *subscription[T] {
	t.mx.Lock()
	defer t.mx.Unlock()

	for _, s := range t.subs {
		if s.worker == w {
			panic("varmq: worker is already subscribed to this topic")
		}
	}

	cursor := t.log.NewCursor()
	view := &topicView[T]{cursor: cursor}
	src := &topicSource[T]{cursor: cursor, w: w, purge: &purgeState{}}
	src.purge.register(w.parkHook())
	s := &subscription[T]{
		// Topic capacity, so IsFull reports this subscription holding the topic full.
		externalBaseQueue: newExternalQueue(view, w, loadQueueConfigs(WithQueueCapacity(t.capacity)), src.purge),
		worker:            w,
		view:              view,
		src:               src,
		t:                 t,
	}

	if t.closed {
		// Inert handle: reads nothing, not registered, not listed.
		cursor.Close()
		return s
	}

	w.addSource(src, priority)
	t.subs = append(t.subs, s)

	return s
}

func (t *topic[T]) unsubscribe(s *subscription[T]) {
	t.mx.Lock()
	defer t.mx.Unlock()

	s.view.cursor.Close()
	t.subs = slices.DeleteFunc(t.subs, func(cur *subscription[T]) bool { return cur == s })
}

// Subscription is a worker's handle on a topic, returned by
// [IWorkerBinder.WithTopic]. It has no Add: the topic is the only producer.
//
// Len and Purge act on what this subscription has not read yet. Purge never
// calls Close on payloads.
//
// IsFull reports whether this subscription has as many unread payloads as the
// topic's WithTopicCapacity, so it is the one making Publish return false. It
// is always false without a capacity.
//
// Close detaches this handle. It is the same as Unsubscribe.
type Subscription[T any] interface {
	IExternalBaseQueue
	// Worker returns the bound worker.
	Worker() Worker
	// Unsubscribe ends the subscription and detaches it from the worker and
	// drops what this subscription has not read. It is idempotent.
	Unsubscribe()
}

type subscription[T any] struct {
	*externalBaseQueue
	worker *worker[T, iJob[T]] // concrete type for removeSource; externalBaseQueue.w is only a Worker
	view   *topicView[T]
	src    *topicSource[T]
	t      *topic[T]
	once   sync.Once
}

func (s *subscription[T]) Unsubscribe() {
	s.once.Do(func() {
		s.t.unsubscribe(s)
		s.worker.removeSource(s.src)
	})
}

// Close detaches the subscription. It is the same as Unsubscribe and exists so
// that the IExternalBaseQueue contract (and `defer sub.Close()`) does the
// expected thing.
func (s *subscription[T]) Close() error {
	s.Unsubscribe()
	return nil
}

// Purge skips everything this subscription has not read. It does not use
// externalBaseQueue.Purge, which calls Close on every pending value that
// implements io.Closer; here those values are user payloads.
func (s *subscription[T]) Purge() {
	s.bumpPurges() // first, so the worker also drops what it already took
	s.view.Purge()
}

// topicView adapts a log cursor to IBaseQueue so subscription can embed
// externalBaseQueue (Len, NumPending, IsFull); subscription overrides Purge
// and Close. The scheduler reads jobs via topicSource on the same cursor.
type topicView[T any] struct {
	cursor *topics.Cursor[T]
}

func (v *topicView[T]) Len() int {
	return v.cursor.Len()
}

// Dequeue hands out the raw payload. Only there to satisfy IBaseQueue.
func (v *topicView[T]) Dequeue() (any, bool) {
	data, ok := v.cursor.Dequeue()
	if !ok {
		return nil, false
	}

	return data, true
}

// Values returns raw payloads, not jobs. Only externalBaseQueue.Purge would
// call it, and Subscription overrides Purge so payloads are never closed.
func (v *topicView[T]) Values() []any {
	vals := v.cursor.Values()
	out := make([]any, len(vals))
	for i, val := range vals {
		out[i] = val
	}

	return out
}

func (v *topicView[T]) Purge() {
	v.cursor.Purge()
}

func (v *topicView[T]) Close() error {
	v.cursor.Close()
	return nil
}
