package mocks

// MockDistributedQueue inherits from MockPersistentQueue and implements IDistributedQueue
type MockDistributedQueue struct {
	*MockPersistentQueue
	Subscribers []func(action string)
}

func NewMockDistributedQueue() *MockDistributedQueue {
	return &MockDistributedQueue{
		MockPersistentQueue: NewMockPersistentQueue(),
		Subscribers:         make([]func(action string), 0),
	}
}

// Implement ISubscribable interface
func (m *MockDistributedQueue) Subscribe(fn func(action string)) {
	m.Subscribers = append(m.Subscribers, fn)
}

// Override Enqueue to notify subscribers
func (m *MockDistributedQueue) Enqueue(item any) bool {
	ok := m.MockPersistentQueue.Enqueue(item)
	if ok {
		// Notify all subscribers about the enqueue action
		for _, subscriber := range m.Subscribers {
			subscriber("enqueued")
		}
	}
	return ok
}

// MockDistributedPriorityQueue inherits from MockPersistentPriorityQueue and implements IDistributedPriorityQueue
type MockDistributedPriorityQueue struct {
	*MockPersistentPriorityQueue
	Subscribers []func(action string)
}

func NewMockDistributedPriorityQueue() *MockDistributedPriorityQueue {
	return &MockDistributedPriorityQueue{
		MockPersistentPriorityQueue: NewMockPersistentPriorityQueue(),
		Subscribers:                 make([]func(action string), 0),
	}
}

// Implement ISubscribable interface
func (m *MockDistributedPriorityQueue) Subscribe(fn func(action string)) {
	m.Subscribers = append(m.Subscribers, fn)
}

// Override Enqueue to notify subscribers
func (m *MockDistributedPriorityQueue) Enqueue(item any, priority int) bool {
	ok := m.MockPersistentPriorityQueue.Enqueue(item, priority)
	if ok {
		// Notify all subscribers about the enqueue action
		for _, subscriber := range m.Subscribers {
			subscriber("enqueued")
		}
	}
	return ok
}
