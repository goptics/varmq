package varmq

import (
	"slices"
	"sync"
)

// PersistentTopic is a topic backed by durable storage with named groups.
// Every group reads every payload; workers bound to the same group share its
// payloads. A group's position survives restarts, so binding the same group
// again resumes where it stopped.
//
// Create one with [NewPersistentTopic]; subscribe a worker with
// [IWorkerBinder.WithPersistentTopic].
type PersistentTopic[T any] interface {
	// Publish serializes data and appends it for every group. configs apply
	// to the job, for example WithJobId for deduplication.
	Publish(data T, configs ...JobConfigFunc) bool
	// NumSubscribers returns the number of subscriptions attached in this
	// process.
	NumSubscribers() int
	// Close detaches every local subscription, keeping their groups, and
	// closes the storage. It is idempotent.
	Close() error

	bindGroup(w *worker[T, iJob[T]], group string, onEvent func(action string)) Subscription[T]
}

// NewPersistentTopic wraps durable topic storage. It panics on nil storage.
func NewPersistentTopic[T any](storage IPersistentTopic) PersistentTopic[T] {
	if storage == nil {
		panic("varmq: NewPersistentTopic called with nil storage")
	}

	return &durableTopic[T]{storage: storage, notifyLocal: true}
}

// durableTopic backs both PersistentTopic and DistributedTopic.
type durableTopic[T any] struct {
	storage IPersistentTopic
	// notifyLocal: Publish wakes this process's subscribers (persistent).
	notifyLocal bool
	// subscribe registers adapter wake-ups for a group (distributed); nil otherwise.
	subscribe func(group string, fn func(action string)) (cancel func())

	mx     sync.RWMutex
	subs   []*durableSubscription[T]
	closed bool
}

func (t *durableTopic[T]) Publish(data T, configs ...JobConfigFunc) bool {
	t.mx.RLock()
	defer t.mx.RUnlock()

	if t.closed {
		return false
	}

	payload, err := newJob(data, loadJobConfigs(newConfig(), configs...)).Json()
	if err != nil {
		return false
	}

	if !t.storage.Publish(payload) {
		return false
	}

	if t.notifyLocal {
		for _, s := range t.subs {
			s.worker.Metrics().incSubmitted()
			s.worker.notifyToPullNextJobs()
		}
	}

	return true
}

func (t *durableTopic[T]) NumSubscribers() int {
	t.mx.RLock()
	defer t.mx.RUnlock()

	return len(t.subs)
}

func (t *durableTopic[T]) Close() error {
	t.mx.Lock()
	if t.closed {
		t.mx.Unlock()
		return nil
	}

	t.closed = true
	subs := slices.Clone(t.subs)
	t.mx.Unlock()

	// Subscription Close takes t.mx itself, so it runs after the unlock.
	for _, s := range subs {
		s.Close()
	}

	return t.storage.Close()
}

// bindGroup joins the group, registers a view of it with w, and records the
// subscription. Lock order: t.mx, then storage, then the worker's queue
// manager.
func (t *durableTopic[T]) bindGroup(w *worker[T, iJob[T]], group string, onEvent func(action string)) Subscription[T] {
	t.mx.Lock()
	defer t.mx.Unlock()

	view := &groupView{storage: t.storage, group: group}
	s := &durableSubscription[T]{
		externalBaseQueue: newExternalQueue(view, w, loadQueueConfigs()),
		worker:            w,
		view:              view,
		t:                 t,
	}

	if t.closed {
		s.inert = true
		return s
	}

	if err := t.storage.JoinGroup(group); err != nil {
		w.sendError(err)
		s.inert = true
		return s
	}

	if t.subscribe != nil {
		s.cancel = t.subscribe(group, onEvent)
	}

	w.queues.Register(view, loadQueueConfigs().priority)
	t.subs = append(t.subs, s)

	// The group may already hold payloads, for example after a restart.
	w.notifyToPullNextJobs()

	return s
}

func (t *durableTopic[T]) remove(s *durableSubscription[T]) {
	t.mx.Lock()
	defer t.mx.Unlock()

	t.subs = slices.DeleteFunc(t.subs, func(cur *durableSubscription[T]) bool { return cur == s })
}

// durableSubscription is a worker's subscription to one group.
type durableSubscription[T any] struct {
	*externalBaseQueue
	worker *worker[T, iJob[T]]
	view   *groupView
	t      *durableTopic[T]
	cancel func() // stops adapter wake-ups; nil for persistent topics
	inert  bool   // never attached: closed topic or failed join

	closeOnce  sync.Once
	deleteOnce sync.Once
}

// Close detaches this subscription and keeps the group's position.
func (s *durableSubscription[T]) Close() error {
	s.closeOnce.Do(func() {
		if s.inert {
			return
		}

		s.t.remove(s)
		if s.cancel != nil {
			s.cancel()
		}
		s.worker.queues.UnregisterItem(s.view)
	})

	return nil
}

// Unsubscribe detaches this subscription and deletes its group, for every
// reader of that group.
func (s *durableSubscription[T]) Unsubscribe() {
	s.Close()

	if s.inert {
		return
	}

	s.deleteOnce.Do(func() {
		if err := s.t.storage.DeleteGroup(s.view.group); err != nil {
			s.worker.sendError(err)
		}
	})
}

// Purge skips the group's unread payloads.
func (s *durableSubscription[T]) Purge() {
	s.view.Purge()
}

// groupView adapts one group of a durable topic adapter to IBaseQueue and
// IAcknowledgeable, so the event loop reads and acknowledges it like a
// persistent queue.
type groupView struct {
	storage IPersistentTopic
	group   string
}

func (v *groupView) Len() int                              { return v.storage.GroupLen(v.group) }
func (v *groupView) Values() []any                         { return v.storage.GroupValues(v.group) }
func (v *groupView) Purge()                                { v.storage.PurgeGroup(v.group) }
func (v *groupView) Close() error                          { return nil }
func (v *groupView) DequeueWithAckId() (any, bool, string) { return v.storage.DequeueGroup(v.group) }
func (v *groupView) Acknowledge(ackID string) bool         { return v.storage.AcknowledgeGroup(v.group, ackID) }

func (v *groupView) Dequeue() (any, bool) {
	item, ok, _ := v.storage.DequeueGroup(v.group)
	return item, ok
}
