package collections

import (
	"sync"
)

// Stack is a generic thread-safe stack implementation using a slice
// It provides LIFO (Last In First Out) operations
type Stack[T any] struct {
	elements []T
	mu       sync.RWMutex
}

// NewStack creates a new empty stack
func NewStack[T any](capacity uint32) *Stack[T] {
	return &Stack[T]{
		elements: make([]T, 0, capacity),
	}
}

// Push adds an element to the top of the stack
func (s *Stack[T]) Push(v T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.elements = append(s.elements, v)
}

// Pop removes and returns the top element from the stack
// If the stack is empty, the zero value for type T is returned along with false
func (s *Stack[T]) Pop() (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero T
	if len(s.elements) == 0 {
		return zero, false
	}

	// Get the last element (top of the stack)
	index := len(s.elements) - 1
	value := s.elements[index]

	// Remove the last element
	s.elements = s.elements[:index]

	return value, true
}

// Peek returns the top element from the stack without removing it
// If the stack is empty, the zero value for type T is returned along with false
func (s *Stack[T]) Peek() (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var zero T
	if len(s.elements) == 0 {
		return zero, false
	}

	// Return the last element without removing it
	return s.elements[len(s.elements)-1], true
}

// Size returns the number of elements in the stack
func (s *Stack[T]) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.elements)
}

// IsEmpty returns true if the stack is empty
func (s *Stack[T]) IsEmpty() bool {
	return s.Size() == 0
}

// Clear removes all elements from the stack
func (s *Stack[T]) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.elements = make([]T, 0, cap(s.elements))
}

// Range iterates through all elements in the stack from top to bottom
// and applies the provided function to each element
func (s *Stack[T]) Range(f func(T)) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Iterate in reverse order (top to bottom)
	for i := len(s.elements) - 1; i >= 0; i-- {
		f(s.elements[i])
	}
}
