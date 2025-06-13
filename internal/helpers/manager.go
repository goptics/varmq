package helpers

import (
	"errors"
	"reflect"
	"sync"
)

// ErrNoItemsRegistered is returned when no items are registered
var ErrNoItemsRegistered = errors.New("no items registered")

// Sizer is an interface for anything that has a Len method
type Sizer interface {
	Len() int
}

// Manager is a generic item manager that manages items implementing the Sizer interface
type Manager[T Sizer] struct {
	items           []T
	roundRobinIndex int
	mx              sync.RWMutex
}

// NewManager creates a new Manager with the specified strategy
func CreateManager[T Sizer]() Manager[T] {
	return Manager[T]{
		items: make([]T, 0),
	}
}

// Register adds a new item to the manager
func (m *Manager[T]) Register(item T) {
	m.mx.Lock()
	defer m.mx.Unlock()

	// Store the item with its priority
	m.items = append(m.items, item)
}

// UnregisterItem removes an item from the manager
// This method uses pointer comparison or reflection to safely compare items
func (m *Manager[T]) UnregisterItem(itemToRemove T) {
	m.mx.Lock()
	defer m.mx.Unlock()

	// Using pointer identity to find and remove the item
	itemToRemovePtr := reflect.ValueOf(itemToRemove).Pointer()

	for i, item := range m.items {
		itemValuePtr := reflect.ValueOf(item).Pointer()

		// Compare memory addresses for reliable equality check
		if itemToRemovePtr == itemValuePtr {
			// Remove by swapping with the last element and then truncating
			lastIndex := len(m.items) - 1
			m.items[i] = m.items[lastIndex]
			m.items = m.items[:lastIndex]

			// Reset the round robin index if it points to the removed item
			if m.roundRobinIndex >= i {
				m.roundRobinIndex = (m.roundRobinIndex + 1) % len(m.items)
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
	for _, item := range m.items {
		totalLen += item.Len()
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

	var maxItem T
	maxLen := -1

	for _, item := range m.items {
		currentLen := item.Len()
		if currentLen > maxLen {
			maxLen = currentLen
			maxItem = item
		}
	}

	return maxItem, nil
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
	for _, item := range m.items {
		currentLen := item.Len()
		if currentLen > 0 && (minLen == -1 || currentLen < minLen) {
			minLen = currentLen
			minItem = item
		}
	}

	// If all items are empty, just pick the first one
	if minLen == -1 && len(m.items) > 0 {
		minItem = m.items[0]
	}

	return minItem, nil
}

// GetRoundRobinItem returns the next item in round-robin order using popFront/pushBack approach
func (m *Manager[T]) GetRoundRobinItem() (T, error) {
	m.mx.Lock()
	defer m.mx.Unlock()

	if len(m.items) == 0 {
		return *new(T), ErrNoItemsRegistered
	}

	item := m.items[m.roundRobinIndex]
	m.roundRobinIndex = (m.roundRobinIndex + 1) % len(m.items)

	return item, nil
}

// Count returns the number of registered items
func (m *Manager[T]) Count() int {
	m.mx.RLock()
	defer m.mx.RUnlock()

	return len(m.items)
}
