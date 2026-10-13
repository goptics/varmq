package mocks

import (
	"errors"
	"strconv"
	"sync"

	"github.com/goptics/varmq/internal/topics"
)

// MockPersistentTopic is an in-memory IPersistentTopic for tests.
type MockPersistentTopic struct {
	mx     sync.Mutex
	log    *topics.Log[any]
	groups map[string]*mockTopicGroup
	acked  []string
	closed bool

	ShouldFailPublish     bool
	ShouldFailAcknowledge bool
	ShouldFailJoin        bool
}

type mockTopicGroup struct {
	cursor *topics.Cursor[any]
	seq    uint64
	subs   map[int]func(action string)
}

// NewMockPersistentTopic returns an empty mock topic.
func NewMockPersistentTopic() *MockPersistentTopic {
	return &MockPersistentTopic{
		log:    topics.NewLog[any](),
		groups: make(map[string]*mockTopicGroup),
	}
}

// Publish appends item for every group and then calls every group's
// subscribed callbacks with "enqueued", outside the lock.
func (m *MockPersistentTopic) Publish(item any) bool {
	m.mx.Lock()
	if m.ShouldFailPublish || m.closed {
		m.mx.Unlock()
		return false
	}

	m.log.Append(item)

	var fns []func(action string)
	for _, g := range m.groups {
		for _, fn := range g.subs {
			fns = append(fns, fn)
		}
	}
	m.mx.Unlock()

	for _, fn := range fns {
		fn("enqueued")
	}

	return true
}

// JoinGroup creates the group at the head if it does not exist.
func (m *MockPersistentTopic) JoinGroup(group string) error {
	m.mx.Lock()
	defer m.mx.Unlock()

	if m.ShouldFailJoin {
		return errors.New("mock: join failed")
	}

	if _, ok := m.groups[group]; !ok {
		m.groups[group] = &mockTopicGroup{
			cursor: m.log.NewCursor(),
			subs:   make(map[int]func(action string)),
		}
	}

	return nil
}

// GroupLen returns the group's unread count, 0 for an unknown group.
func (m *MockPersistentTopic) GroupLen(group string) int {
	m.mx.Lock()
	defer m.mx.Unlock()

	if g := m.groups[group]; g != nil {
		return g.cursor.Len()
	}

	return 0
}

// GroupValues returns the group's unread messages.
func (m *MockPersistentTopic) GroupValues(group string) []any {
	m.mx.Lock()
	defer m.mx.Unlock()

	if g := m.groups[group]; g != nil {
		return g.cursor.Values()
	}

	return nil
}

// DequeueGroup returns the next message with a per-group sequence number as
// its ack ID.
func (m *MockPersistentTopic) DequeueGroup(group string) (any, bool, string) {
	m.mx.Lock()
	defer m.mx.Unlock()

	g := m.groups[group]
	if g == nil {
		return nil, false, ""
	}

	item, ok := g.cursor.Dequeue()
	if !ok {
		return nil, false, ""
	}

	g.seq++

	return item, true, strconv.FormatUint(g.seq, 10)
}

// AcknowledgeGroup records "group:ackID".
func (m *MockPersistentTopic) AcknowledgeGroup(group, ackID string) bool {
	m.mx.Lock()
	defer m.mx.Unlock()

	if m.ShouldFailAcknowledge {
		return false
	}

	m.acked = append(m.acked, group+":"+ackID)

	return true
}

// PurgeGroup skips the group to the head.
func (m *MockPersistentTopic) PurgeGroup(group string) {
	m.mx.Lock()
	defer m.mx.Unlock()

	if g := m.groups[group]; g != nil {
		g.cursor.Purge()
	}
}

// DeleteGroup forgets the group.
func (m *MockPersistentTopic) DeleteGroup(group string) error {
	m.mx.Lock()
	defer m.mx.Unlock()

	if g := m.groups[group]; g != nil {
		g.cursor.Close()
		delete(m.groups, group)
	}

	return nil
}

// Close makes further publishes fail.
func (m *MockPersistentTopic) Close() error {
	m.mx.Lock()
	defer m.mx.Unlock()

	m.closed = true

	return nil
}

// HasGroup reports whether the group exists.
func (m *MockPersistentTopic) HasGroup(group string) bool {
	m.mx.Lock()
	defer m.mx.Unlock()

	_, ok := m.groups[group]

	return ok
}

// AckedIDs returns a copy of the recorded acknowledgements.
func (m *MockPersistentTopic) AckedIDs() []string {
	m.mx.Lock()
	defer m.mx.Unlock()

	return append([]string(nil), m.acked...)
}
