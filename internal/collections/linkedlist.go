package collections

import (
	"sync"
)

// Node represents a node in a doubly linked list
type Node[T any] struct {
	Value T
	next  *Node[T]
	prev  *Node[T]
	mx    sync.RWMutex
}

// NewNode creates a new Node with the given value
// Note: This creates a standalone node that is not part of any list yet.
// Use list methods like PushBack, PushFront, etc. to add it to a list.
func NewNode[T any](value T) *Node[T] {
	return &Node[T]{
		Value: value,
	}
}

// Next returns the next node in the list
func (n *Node[T]) Next() *Node[T] {
	n.mx.RLock()
	defer n.mx.RUnlock()

	if n.next != nil {
		return n.next
	}
	return nil
}

// Prev returns the previous node in the list
func (n *Node[T]) Prev() *Node[T] {
	n.mx.RLock()
	defer n.mx.RUnlock()

	if n.prev != nil {
		return n.prev
	}
	return nil
}

// List represents a doubly linked list
type List[T any] struct {
	root Node[T]      // sentinel node, only &root, root.prev, and root.next are used
	len  int          // current list length excluding sentinel node
	mx   sync.RWMutex // protects the list structure
}

// Init initializes or clears list l
func (l *List[T]) Init() *List[T] {
	l.mx.Lock()
	defer l.mx.Unlock()

	l.root.next = &l.root
	l.root.prev = &l.root
	l.len = 0
	return l
}

// NewList returns an initialized list
func NewList[T any]() *List[T] {
	return new(List[T]).Init()
}

// Len returns the number of elements of list l
func (l *List[T]) Len() int {
	l.mx.RLock()
	defer l.mx.RUnlock()

	return l.len
}

// Front returns the first element of list l or nil if the list is empty
func (l *List[T]) Front() *Node[T] {
	l.mx.RLock()
	defer l.mx.RUnlock()

	if l.len == 0 {
		return nil
	}
	return l.root.next
}

// Back returns the last element of list l or nil if the list is empty
func (l *List[T]) Back() *Node[T] {
	l.mx.RLock()
	defer l.mx.RUnlock()

	if l.len == 0 {
		return nil
	}
	return l.root.prev
}

// insertValue is a helper function that creates a new Node with value v and inserts it after at
func (l *List[T]) insertValue(v T, at *Node[T]) *Node[T] {
	return l.insert(NewNode(v), at)
}

// insert inserts node after at, increments l.len, and returns node
func (l *List[T]) insert(node, at *Node[T]) *Node[T] {
	// Assumes caller has already acquired lock
	node.prev = at
	node.next = at.next
	at.next.prev = node
	at.next = node
	l.len++
	return node
}

// InsertAfter inserts a new node node after mark and returns node
// Note: With the list field removed, we can't verify the node belongs to this list
// The caller must ensure the mark belongs to this list
// The mark must not be nil.
func (l *List[T]) InsertAfter(v T, mark *Node[T]) *Node[T] {
	l.mx.Lock()
	defer l.mx.Unlock()

	// We can no longer verify if mark belongs to this list
	return l.insertValue(v, mark)
}

// PushFront inserts a new node at the front of list l and returns the new node
func (l *List[T]) PushFront(v T) *Node[T] {
	l.mx.Lock()
	defer l.mx.Unlock()

	return l.insertValue(v, &l.root)
}

// PushBack inserts a new node at the back of list l and returns the new node
func (l *List[T]) PushBack(v T) *Node[T] {
	l.mx.Lock()
	defer l.mx.Unlock()

	return l.insertValue(v, l.root.prev)
}

// PushNode inserts an existing node at the back of the list
// This is the key function that allows reinserting a removed node
func (l *List[T]) PushNode(n *Node[T]) {
	l.mx.Lock()
	defer l.mx.Unlock()

	n.next = &l.root
	n.prev = l.root.prev
	l.root.prev.next = n
	l.root.prev = n
	l.len++
}

// Remove removes node from its list, decrements l.len, and returns the node
// The node can be reinserted into a list using PushNode
// Note: With the list field removed, we can't verify the node belongs to this list
func (l *List[T]) Remove(node *Node[T]) bool {
	l.mx.Lock()
	defer l.mx.Unlock()

	// Skip this operation if the node is the sentinel node or not properly connected
	if node == &l.root || node.prev == nil || node.next == nil {
		return false
	}

	// Perform removal (this works for any node in the list)
	node.prev.next = node.next
	node.next.prev = node.prev
	node.next = nil // avoid memory leaks
	node.prev = nil // avoid memory leaks
	l.len--
	return true
}

// PopBack removes and returns the last element of list l
// If the list is empty, it returns nil
func (l *List[T]) PopBack() *Node[T] {
	l.mx.Lock()
	defer l.mx.Unlock()

	if l.len == 0 {
		return nil
	}

	last := l.root.prev
	if last != &l.root { // Check that it's not the sentinel node
		last.prev.next = last.next
		last.next.prev = last.prev
		last.next = nil // avoid memory leaks
		last.prev = nil // avoid memory leaks
		l.len--
	}

	return last
}

// PopFront removes and returns the first element of list l
// If the list is empty, it returns nil
func (l *List[T]) PopFront() *Node[T] {
	l.mx.Lock()
	defer l.mx.Unlock()

	if l.len == 0 {
		return nil
	}

	first := l.root.next
	if first != &l.root { // Check that it's not the sentinel node
		first.prev.next = first.next
		first.next.prev = first.prev
		first.next = nil // avoid memory leaks
		first.prev = nil // avoid memory leaks
		l.len--
	}

	return first
}

// NodeSlice returns a slice containing all nodes in the list
// This is useful for safely iterating and potentially modifying the list
func (l *List[T]) NodeSlice() []*Node[T] {
	l.mx.RLock()
	defer l.mx.RUnlock()

	// Create a slice with exact capacity
	nodes := make([]*Node[T], 0, l.len)

	// Add all nodes to the slice
	for node := l.root.next; node != &l.root; node = node.next {
		nodes = append(nodes, node)
	}

	return nodes
}
