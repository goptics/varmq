package queue

// heapQueue implements heap.Interface for a slice of *EnqItem[T].
type heapQueue[T any] struct {
	items []*EnqItem[T]
}

// Len, Less, Swap are standard for heap.Interface.
func (pq *heapQueue[T]) Len() int {
	return len(pq.items)
}

// Less: smaller Priority = higher priority (pops first).
func (pq *heapQueue[T]) Less(i, j int) bool {
	if pq.items[i].Priority == pq.items[j].Priority {
		// Tie-breaker: lower insertion index => higher priority
		return pq.items[i].Index < pq.items[j].Index
	}
	return pq.items[i].Priority < pq.items[j].Priority
}

// Swap swaps two items in the heap array.
func (pq *heapQueue[T]) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
}

// Push is called by heap.Push to add a new element to the end.
// Time complexity: O(1)
func (pq *heapQueue[T]) Push(x any) {
	pq.items = append(pq.items, x.(*EnqItem[T]))
}

// Pop is called by heap.Pop to remove the last element from the slice.
// Time complexity: O(1)
func (pq *heapQueue[T]) Pop() any {
	n := len(pq.items)
	item := pq.items[n-1]
	pq.items = pq.items[:n-1]
	return item
}
