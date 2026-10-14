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
// Workers sharing a group may occasionally report ErrFailedToDequeue to their
// error handler when another worker took the message first; it is harmless.
// The Submitted metric counts a message once per local subscription, even when
// one group processes it once.
//
// Create one with [NewPersistentTopic]; subscribe a worker with
// [IWorkerBinder.WithPersistentTopic].
type PersistentTopic[T any] interface {
	// Publish serializes data and appends it for every group. configs apply
	// to the job, for example WithJobId for deduplication. It returns false
	// when the topic is closed, the payload cannot be serialized, or the
	// storage rejects it. True does not mean any group exists to read it.
	Publish(data T, configs ...JobConfigFunc) bool
	// NumSubscribers returns the number of subscriptions attached in this
	// process.
	NumSubscribers() int
	// Close detaches every local subscription, keeping their groups, and
	// closes the storage. It is idempotent. Stop the workers first:
	// acknowledgements for jobs still running after Close fail and those
	// messages are delivered again.
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

// bindGroup joins the group, binds a view of it to w as a source, and records
// the subscription. Lock order: t.mx, then storage, then the worker's source
// manager.
func (t *durableTopic[T]) bindGroup(w *worker[T, iJob[T]], group string, onEvent func(action string)) Subscription[T] {
	view := &groupView{storage: t.storage, group: group}
	s := &durableSubscription[T]{
		externalBaseQueue: newExternalQueue(view, w, loadQueueConfigs()),
		worker:            w,
		view:              view,
		t:                 t,
	}

	t.mx.Lock()

	if t.closed {
		t.mx.Unlock()
		view.inert = true
		s.inert = true
		return s
	}

	if err := t.storage.JoinGroup(group); err != nil {
		t.mx.Unlock()
		view.inert = true
		s.inert = true
		// Sent after unlocking: the user's handler may call back into the topic.
		w.sendError(err)
		return s
	}

	if t.subscribe != nil {
		s.cancel = t.subscribe(group, onEvent)
	}

	s.src = w.bind(view, loadQueueConfigs().priority)
	t.subs = append(t.subs, s)

	// The group may already hold payloads, for example after a restart.
	w.notifyToPullNextJobs()
	t.mx.Unlock()

	return s
}

func (t *durableTopic[T]) remove(s *durableSubscription[T]) {
	t.mx.Lock()
	defer t.mx.Unlock()

	t.subs = slices.DeleteFunc(t.subs, func(cur *durableSubscription[T]) bool { return cur == s })
}

// groupSubs returns the local subscriptions attached to group.
func (t *durableTopic[T]) groupSubs(group string) []*durableSubscription[T] {
	t.mx.RLock()
	defer t.mx.RUnlock()

	var out []*durableSubscription[T]
	for _, s := range t.subs {
		if s.view.group == group {
			out = append(out, s)
		}
	}

	return out
}

// durableSubscription is a worker's subscription to one group.
type durableSubscription[T any] struct {
	*externalBaseQueue
	worker *worker[T, iJob[T]]
	view   *groupView
	src    source[iJob[T]]
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
		s.worker.sources.UnregisterItem(s.src)
	})

	return nil
}

// Unsubscribe detaches this subscription and deletes its group, which also
// detaches every subscription on that group in this process.
func (s *durableSubscription[T]) Unsubscribe() {
	s.Close()

	if s.inert {
		return
	}

	s.deleteOnce.Do(func() {
		// Close takes t.mx itself, so siblings are collected first and closed
		// after the lock is released.
		for _, sib := range s.t.groupSubs(s.view.group) {
			// a detached sibling must not delete a group someone binds later
			sib.deleteOnce.Do(func() {})
			sib.Close()
		}

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
// IAcknowledgeable, so an ackSource reads and acknowledges it like a
// persistent queue.
type groupView struct {
	storage IPersistentTopic
	group   string
	// inert views never reach storage: the handle was never attached, so the
	// group it names may belong to another reader.
	inert bool
}

func (v *groupView) Len() int {
	if v.inert {
		return 0
	}

	return v.storage.GroupLen(v.group)
}

// Values returns nil without calling storage: it is only reachable through
// externalBaseQueue.Purge, which durableSubscription overrides.
func (v *groupView) Values() []any { return nil }

func (v *groupView) Purge() {
	if v.inert {
		return
	}

	v.storage.PurgeGroup(v.group)
}

func (v *groupView) Close() error { return nil }

func (v *groupView) DequeueWithAckId() (any, bool, string) {
	if v.inert {
		return nil, false, ""
	}

	return v.storage.DequeueGroup(v.group)
}

func (v *groupView) Acknowledge(ackID string) bool {
	if v.inert {
		return false
	}

	return v.storage.AcknowledgeGroup(v.group, ackID)
}

func (v *groupView) Dequeue() (any, bool) {
	item, ok, _ := v.DequeueWithAckId()
	return item, ok
}
