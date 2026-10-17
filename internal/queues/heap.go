package queues

// heapQueue is a binary min-heap of enqItem[T] stored by value, so an
// Enqueue costs one slice append and no per-item allocation. The ordering is
// that of container/heap with the same Less: lower Priority first, then lower
// insertion Index.
type heapQueue[T any] struct {
	items []enqItem[T]
}

// Len returns the number of items in the heap.
func (pq *heapQueue[T]) Len() int {
	return len(pq.items)
}

// less reports whether items[i] must pop before items[j].
func (pq *heapQueue[T]) less(i, j int) bool {
	a, b := &pq.items[i], &pq.items[j]
	if a.Priority == b.Priority {
		return a.Index < b.Index
	}
	return a.Priority < b.Priority
}

// push appends item and restores the heap property. O(log n).
func (pq *heapQueue[T]) push(item enqItem[T]) {
	pq.items = append(pq.items, item)
	pq.up(len(pq.items) - 1)
}

// pop removes and returns the minimum item. The caller checks Len() > 0.
// The vacated slot is zeroed so a dequeued value is not kept alive. O(log n).
func (pq *heapQueue[T]) pop() enqItem[T] {
	n := len(pq.items) - 1
	pq.items[0], pq.items[n] = pq.items[n], pq.items[0]
	pq.down(0, n)
	item := pq.items[n]
	pq.items[n] = enqItem[T]{}
	pq.items = pq.items[:n]
	return item
}

func (pq *heapQueue[T]) up(j int) {
	for {
		i := (j - 1) / 2 // parent
		if i == j || !pq.less(j, i) {
			break
		}
		pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
		j = i
	}
}

// container/heap guards 2*i+1 against int overflow; a slice cannot reach
// 2^62 elements, so the check is omitted.
func (pq *heapQueue[T]) down(i, n int) {
	for {
		j := 2*i + 1 // left child
		if j >= n {
			break
		}
		if r := j + 1; r < n && pq.less(r, j) {
			j = r // right child
		}
		if !pq.less(j, i) {
			break
		}
		pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
		i = j
	}
}
