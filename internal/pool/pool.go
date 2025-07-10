package pool

import (
	"sync"
	"time"
)

// Pool represents a stack-based pool with two slices
type Pool[T any] struct {
	idle       []*Node[T]   // idle stack (up to capacity)
	expired    []*Node[T]   // expired stack (overflow)
	idleCap    int          // Maximum capacity for idle stack
	expiredCap int          // Maximum capacity for expired stack
	Cache      sync.Pool    // Cache for reusing Node instances
	mu         sync.RWMutex // Protects both slices
}

func New[T any](cap, maxCap int) *Pool[T] {
	return &Pool[T]{
		idle:       make([]*Node[T], 0, cap),
		expired:    make([]*Node[T], 0, maxCap),
		idleCap:    cap,
		expiredCap: maxCap,
		Cache: sync.Pool{
			New: func() any {
				return NewNode[T](1)
			},
		},
	}
}

// Len returns the composite length of both slices
func (p *Pool[T]) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.idle) + len(p.expired)
}

// ChangeCapacity updates the capacity and maxCapacity of the pool
func (p *Pool[T]) ChangeCapacities(idleCap, expiredCap int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.idleCap = idleCap
	p.expiredCap = expiredCap
}

// PushNode adds a node to the pool following stack behavior
// If idle slice is at capacity, pushes to expired slice
func (p *Pool[T]) PushNode(node *Node[T]) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.idle) < p.idleCap {
		p.idle = append(p.idle, node)
	} else {
		p.expired = append(p.expired, node)
	}
}

// PopBack removes and returns a node following stack behavior
// Pops from idle slice first, if empty pops from expired slice
func (p *Pool[T]) PopBack() *Node[T] {
	p.mu.Lock()
	defer p.mu.Unlock()

	l, il := len(p.idle), len(p.expired)

	// Try to pop from idle slice first
	if l > 0 {
		node := p.idle[l-1]
		p.idle[l-1] = nil
		p.idle = p.idle[:l-1]
		return node
	}

	// If idle is empty, pop from expired slice
	if il > 0 {
		node := p.expired[il-1]
		p.expired[il-1] = nil
		p.expired = p.expired[:il-1]
		return node
	}

	// Both slices are empty
	return nil
}

// removeExpired removes expired workers using binary search and divide-and-conquer approach
// It removes nodes that have been expired longer than the specified duration
func (p *Pool[T]) RemoveExpired(expiredDuration time.Duration) []*Node[T] {
	p.mu.Lock()
	defer p.mu.Unlock()

	cutoffTime := time.Now().Add(-expiredDuration)
	removedNodes := make([]*Node[T], 0)

	// Process idle slice - remove expired nodes beyond target count
	if len(p.idle) > p.idleCap {
		if expiredNodes := p.findExpiredNodes(p.idle[p.idleCap:], cutoffTime); len(expiredNodes) > 0 {
			// Remove expired nodes from idle slice
			p.idle = p.removeExpiredFromSlice(p.idle, expiredNodes, p.idleCap)
			removedNodes = append(removedNodes, expiredNodes...)
		}
	}

	if expiredNodes := p.findExpiredNodes(p.expired, cutoffTime); len(expiredNodes) > 0 {
		// Remove expired nodes from expired slice
		p.expired = p.removeExpiredFromSlice(p.expired, expiredNodes, p.expiredCap)
		removedNodes = append(removedNodes, expiredNodes...)
	}

	return removedNodes
}

// findExpiredNodes uses binary search to find nodes that are expired
// Since nodes are already sorted by FIFO order (naturally sorted by lastUsed time)
func (p *Pool[T]) findExpiredNodes(nodes []*Node[T], cutoffTime time.Time) []*Node[T] {
	if len(nodes) == 0 {
		return nil
	}

	// Binary search to find the first expired node
	// Since nodes are in FIFO order, older nodes (more likely to be expired) are at the beginning
	left, right := 0, len(nodes)

	for left < right {
		mid := (left + right) / 2
		if nodes[mid].GetLastUsed().After(cutoffTime) {
			left = mid + 1
		} else {
			right = mid
		}
	}

	// All nodes from 'left' index onwards are expired
	if left < len(nodes) {
		expired := make([]*Node[T], len(nodes)-left)
		copy(expired, nodes[left:])
		return expired
	}

	return nil
}

// removeExpiredFromSlice removes expired nodes from the slice using divide-and-conquer
func (p *Pool[T]) removeExpiredFromSlice(slice []*Node[T], expiredNodes []*Node[T], cap int) []*Node[T] {
	if len(expiredNodes) == 0 {
		return slice
	}

	// Create a map for O(1) lookup of expired nodes
	expiredMap := make(map[*Node[T]]struct{}, len(expiredNodes))
	for _, node := range expiredNodes {
		expiredMap[node] = struct{}{}
	}

	// Filter out expired nodes
	result := make([]*Node[T], 0, cap)
	for _, node := range slice {
		if _, ok := expiredMap[node]; !ok {
			result = append(result, node)
		}
	}

	return result
}
