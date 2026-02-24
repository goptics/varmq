package helpers

import (
	"errors"
	"reflect"
	"slices"
	"sync"
)

var (
	ErrNoItemsRegistered = errors.New("no items registered")
	ErrAllItemsEmpty     = errors.New("all items are empty")
)

// Sizer is an interface for anything that has a Len method
type Sizer interface {
	Len() int
}

type registered[T Sizer] struct {
	item     T
	priority int
}

// Manager is a generic item manager that manages items implementing the Sizer interface
type Manager[T Sizer] struct {
	items           []registered[T]
	roundRobinIndex int
	mx              sync.RWMutex
}

// NewManager creates a new Manager
func NewManager[T Sizer]() *Manager[T] {
	return &Manager[T]{
		items: make([]registered[T], 0),
	}
}

// Register adds a new item to the manager.
// Optional priorities can be provided. The lowest number means highest priority and will be placed at the
// beginning of the list. Default priority is math.MaxInt.
func (m *Manager[T]) Register(item T, priority int) {
	m.mx.Lock()
	defer m.mx.Unlock()

	r := registered[T]{
		item:     item,
		priority: priority,
	}

	// Insert into the right position to maintain priority (ascending order: lower number = higher priority)
	// and preserve insertion order for same priorities
	insertIdx := len(m.items)
	for i, existing := range m.items {
		if priority < existing.priority {
			insertIdx = i
			break
		}
	}

	m.items = slices.Insert(m.items, insertIdx, r)
}

// UnregisterItem removes an item from the manager
// This method uses pointer comparison or reflection to safely compare items
func (m *Manager[T]) UnregisterItem(itemToRemove T) {
	m.mx.Lock()
	defer m.mx.Unlock()

	// Using pointer identity to find and remove the item
	itemToRemovePtr := reflect.ValueOf(itemToRemove).Pointer()

	for i, r := range m.items {
		itemValuePtr := reflect.ValueOf(r.item).Pointer()

		// Compare memory addresses for reliable equality check
		if itemToRemovePtr == itemValuePtr {
			// Remove using slices.Delete to maintain priority ordering
			m.items = slices.Delete(m.items, i, i+1)

			// Reset the round robin index if it points to or after the removed item
			if m.roundRobinIndex >= i {
				m.roundRobinIndex = 0
			}

			return
		}
	}
}

// Len returns the total length of all items
func (m *Manager[T]) Len() int {
	m.mx.RLock()
	defer m.mx.RUnlock()

	totalLen := 0
	for _, r := range m.items {
		totalLen += r.item.Len()
	}

	return totalLen
}

// GetMaxLenItem returns the item with the maximum length
func (m *Manager[T]) GetMaxLenItem() (T, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	if len(m.items) == 0 {
		return *new(T), ErrNoItemsRegistered
	}

	maxItem := slices.MaxFunc(m.items, func(a, b registered[T]) int {
		return a.item.Len() - b.item.Len()
	})

	if maxItem.item.Len() == 0 {
		return *new(T), ErrAllItemsEmpty
	}

	return maxItem.item, nil
}

// GetMinLenItem returns the item with the minimum length (excluding empty items unless all are empty)
func (m *Manager[T]) GetMinLenItem() (T, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	if len(m.items) == 0 {
		return *new(T), ErrNoItemsRegistered
	}

	var minItem T
	minLen := -1

	// First try to find the minimum length excluding empty items
	for _, r := range m.items {
		l := r.item.Len()
		if l > 0 && (minLen == -1 || l < minLen) {
			minLen = l
			minItem = r.item
		}
	}

	// If no non-empty items found, all items are empty
	if minLen == -1 {
		return *new(T), ErrAllItemsEmpty
	}

	return minItem, nil
}

// GetPriorityItem returns the first item with length > 0 based on priority (highest priority first).
// Since items are inherently stored sorted by priority (lowest number first), it simply returns the first non-empty item.
func (m *Manager[T]) GetPriorityItem() (T, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	if len(m.items) == 0 {
		return *new(T), ErrNoItemsRegistered
	}

	for _, r := range m.items {
		if r.item.Len() > 0 {
			return r.item, nil
		}
	}

	return *new(T), ErrAllItemsEmpty
}

// GetRoundRobinItem returns the next item in round-robin order
func (m *Manager[T]) GetRoundRobinItem() (T, error) {
	m.mx.Lock()
	defer m.mx.Unlock()

	if len(m.items) == 0 {
		return *new(T), ErrNoItemsRegistered
	}

	start := m.roundRobinIndex

	for {
		r := m.items[m.roundRobinIndex]
		m.roundRobinIndex = (m.roundRobinIndex + 1) % len(m.items)

		if r.item.Len() > 0 {
			return r.item, nil
		}

		if m.roundRobinIndex == start {
			return *new(T), ErrAllItemsEmpty
		}
	}
}

// Count returns the number of registered items
func (m *Manager[T]) Count() int {
	m.mx.RLock()
	defer m.mx.RUnlock()

	return len(m.items)
}
