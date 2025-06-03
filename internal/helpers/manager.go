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

// Item[T] stores an item along with its priority
type Item[T Sizer] struct {
	Value    T
	Priority int
}

// Manager is a generic item manager that manages items implementing the Sizer interface
type Manager[T Sizer] struct {
	items []Item[T]
	mx    sync.RWMutex
}

// NewManager creates a new Manager with the specified strategy
func CreateManager[T Sizer]() Manager[T] {
	return Manager[T]{
		items: make([]Item[T], 0),
	}
}

// Register adds a new item to the manager
func (m *Manager[T]) Register(item T, priority int) {
	m.mx.Lock()
	defer m.mx.Unlock()

	// Store the item with its priority
	m.items = append(m.items, Item[T]{
		Value:    item,
		Priority: priority,
	})
}

// UnregisterItem removes an item from the manager
// This method uses pointer comparison or reflection to safely compare items
func (m *Manager[T]) UnregisterItem(itemToRemove T) {
	m.mx.Lock()
	defer m.mx.Unlock()

	// Using pointer identity to find and remove the item
	itemToRemovePtr := reflect.ValueOf(itemToRemove).Pointer()

	for i, item := range m.items {
		itemValuePtr := reflect.ValueOf(item.Value).Pointer()

		// Compare memory addresses for reliable equality check
		if itemToRemovePtr == itemValuePtr {
			// Remove by swapping with the last element and then truncating
			lastIndex := len(m.items) - 1
			m.items[i] = m.items[lastIndex]
			m.items = m.items[:lastIndex]
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
		totalLen += item.Value.Len()
	}

	return totalLen
}

// GetMaxLenItem returns the item with the maximum length
func (m *Manager[T]) GetMaxLenItem() (T, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	// Zero value to return in case of error
	var zero T

	if len(m.items) == 0 {
		return zero, ErrNoItemsRegistered
	}

	var maxItem T
	maxLen := -1

	for _, item := range m.items {
		currentLen := item.Value.Len()
		if currentLen > maxLen {
			maxLen = currentLen
			maxItem = item.Value
		}
	}

	return maxItem, nil
}

// GetMinLenItem returns the item with the minimum length (excluding empty items unless all are empty)
func (m *Manager[T]) GetMinLenItem() (T, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	// Zero value to return in case of error
	var zero T

	if len(m.items) == 0 {
		return zero, ErrNoItemsRegistered
	}

	var minItem T
	minLen := -1

	// First try to find the minimum length excluding empty items
	for _, item := range m.items {
		currentLen := item.Value.Len()
		if currentLen > 0 && (minLen == -1 || currentLen < minLen) {
			minLen = currentLen
			minItem = item.Value
		}
	}

	// If all items are empty, just pick the first one
	if minLen == -1 && len(m.items) > 0 {
		minItem = m.items[0].Value
	}

	return minItem, nil
}

// GetRoundRobinItem returns the next item in round-robin order using popFront/pushBack approach
func (m *Manager[T]) GetRoundRobinItem() (T, error) {
	m.mx.Lock()
	defer m.mx.Unlock()

	// Zero value to return in case of error
	var zero T

	if len(m.items) == 0 {
		return zero, ErrNoItemsRegistered
	}

	// Get the first item
	firstItem := m.items[0].Value

	// Move it to the back to implement round-robin
	if len(m.items) > 1 {
		m.items = append(m.items[1:], m.items[0])
	}

	return firstItem, nil
}

// GetPriorityItem returns the item with the highest priority
func (m *Manager[T]) GetPriorityItem() (T, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	// Zero value to return in case of error
	var zero T

	if len(m.items) == 0 {
		return zero, ErrNoItemsRegistered
	}

	var priorityItem T
	highestPriority := -1

	for _, item := range m.items {
		if item.Priority > highestPriority {
			highestPriority = item.Priority
			priorityItem = item.Value
		}
	}

	return priorityItem, nil
}

// Count returns the number of registered items
func (m *Manager[T]) Count() int {
	m.mx.RLock()
	defer m.mx.RUnlock()

	return len(m.items)
}
