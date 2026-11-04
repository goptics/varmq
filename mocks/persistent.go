package mocks

import (
	"sync"

	"github.com/goptics/varmq/internal/queues"
)

// Outcomes records the Acknowledge and Reject calls a mock storage got.
type Outcomes struct {
	mu       sync.Mutex
	acked    []string
	rejected []string
}

// Acked returns the ack ids passed to Acknowledge, in call order.
func (o *Outcomes) Acked() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.acked...)
}

// Rejected returns the reasons passed to Reject, in call order.
func (o *Outcomes) Rejected() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.rejected...)
}

func (o *Outcomes) ack(ackID string) {
	o.mu.Lock()
	o.acked = append(o.acked, ackID)
	o.mu.Unlock()
}

func (o *Outcomes) reject(reason string) {
	o.mu.Lock()
	o.rejected = append(o.rejected, reason)
	o.mu.Unlock()
}

// MockPersistentQueue embeds the internal queue and implements IPersistentQueue
type MockPersistentQueue struct {
	*queues.Queue[any]
	ShouldFailEnqueue     bool
	ShouldFailAcknowledge bool
	ShouldFailReject      bool
	ShouldFailDequeue     bool
	Outcomes
}

func NewMockPersistentQueue() *MockPersistentQueue {
	return &MockPersistentQueue{
		Queue: queues.NewQueue[any](),
	}
}

// Implement IAcknowledgeable interface
func (m *MockPersistentQueue) Acknowledge(ackID string) bool {
	if m.ShouldFailAcknowledge {
		return false
	}
	m.ack(ackID)
	return true
}

func (m *MockPersistentQueue) Reject(ackID string, reason string) bool {
	if m.ShouldFailReject {
		return false
	}
	m.reject(reason)
	return true
}

// Override Enqueue to simulate failure
func (m *MockPersistentQueue) Enqueue(item any) bool {
	if m.ShouldFailEnqueue {
		return false
	}
	return m.Queue.Enqueue(item)
}

// EnqueueOnce shadows the embedded in-memory queue's, which would track ids
// it can never free (the items here are JSON bytes, with no ID method) and
// skip the overrides above. This mock does not deduplicate.
func (m *MockPersistentQueue) EnqueueOnce(item any, _ string) bool {
	return m.Enqueue(item)
}

func (m *MockPersistentQueue) DequeueWithAckId() (any, bool, string) {
	if m.ShouldFailDequeue {
		return nil, false, ""
	}
	// Mock implementation - return item with mock ack ID
	item, ok := m.Queue.Dequeue()
	return item, ok, "mock-ack-id"
}

// MockPersistentPriorityQueue embeds the internal priority queue and implements IPersistentPriorityQueue
type MockPersistentPriorityQueue struct {
	*queues.PriorityQueue[any]
	ShouldFailEnqueue     bool
	ShouldFailAcknowledge bool
	ShouldFailReject      bool
	ShouldFailDequeue     bool
	Outcomes
}

func NewMockPersistentPriorityQueue() *MockPersistentPriorityQueue {
	return &MockPersistentPriorityQueue{
		PriorityQueue: queues.NewPriorityQueue[any](),
	}
}

// Implement IAcknowledgeable interface
func (m *MockPersistentPriorityQueue) Acknowledge(ackID string) bool {
	if m.ShouldFailAcknowledge {
		return false
	}
	m.ack(ackID)
	return true
}

func (m *MockPersistentPriorityQueue) Reject(ackID string, reason string) bool {
	if m.ShouldFailReject {
		return false
	}
	m.reject(reason)
	return true
}

// Override Enqueue to simulate failure
func (m *MockPersistentPriorityQueue) Enqueue(item any, priority int) bool {
	if m.ShouldFailEnqueue {
		return false
	}
	return m.PriorityQueue.Enqueue(item, priority)
}

// EnqueueOnce shadows the embedded in-memory queue's; see
// MockPersistentQueue.EnqueueOnce.
func (m *MockPersistentPriorityQueue) EnqueueOnce(item any, priority int, _ string) bool {
	return m.Enqueue(item, priority)
}

func (m *MockPersistentPriorityQueue) DequeueWithAckId() (any, bool, string) {
	if m.ShouldFailDequeue {
		return nil, false, ""
	}
	// Mock implementation - return item with mock ack ID
	item, ok := m.PriorityQueue.Dequeue()
	return item, ok, "mock-ack-id"
}
