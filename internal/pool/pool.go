package pool

import (
	"sync"
	"sync/atomic"
)

// Elem is one idle worker in a Pool.
type Elem[T any] struct {
	Value Node[T]
	next  atomic.Pointer[Elem[T]]
}

// Pool is a LIFO stack of idle workers. Pushes are lock free, so workers
// finishing at once never wait on each other. Removals (PopBack, Prune)
// serialise on a mutex: with a single remover at a time, a node cannot be
// popped and pushed back between a remover's load and its CAS, so the CAS
// cannot succeed on a stale head (no ABA).
type Pool[T any] struct {
	head  atomic.Pointer[Elem[T]]
	len   atomic.Int64
	popMx sync.Mutex
	Cache sync.Pool
}

func New[T any](cap int) *Pool[T] {
	return &Pool[T]{
		Cache: sync.Pool{
			New: func() any {
				return &Elem[T]{Value: CreateNode[T](cap)}
			},
		},
	}
}

// Len returns the number of idle workers. A push counts itself before it
// lands, so a racing Len can be one high, never negative.
func (p *Pool[T]) Len() int {
	return int(p.len.Load())
}

// PushNode puts e on top of the stack.
func (p *Pool[T]) PushNode(e *Elem[T]) {
	p.len.Add(1)
	for {
		h := p.head.Load()
		e.next.Store(h)
		if p.head.CompareAndSwap(h, e) {
			return
		}
	}
}

// PopBack removes and returns the most recently pushed node, or nil.
func (p *Pool[T]) PopBack() *Elem[T] {
	p.popMx.Lock()
	defer p.popMx.Unlock()

	for {
		h := p.head.Load()
		if h == nil {
			return nil
		}
		if p.head.CompareAndSwap(h, h.next.Load()) {
			p.len.Add(-1)
			return h
		}
	}
}

// Prune removes and returns the nodes drop reports, leaving the bottom keep
// nodes (the longest idle) alone. Nodes pushed while it runs are skipped.
func (p *Pool[T]) Prune(keep int, drop func(*Elem[T]) bool) []*Elem[T] {
	p.popMx.Lock()
	defer p.popMx.Unlock()

	top := p.head.Load()
	n := 0
	for e := top; e != nil; e = e.next.Load() {
		n++
	}

	var dropped []*Elem[T]
	var prev *Elem[T] // nearest kept node above e, nil while none
	for e, i := top, 0; e != nil && i < n-keep; i++ {
		next := e.next.Load()
		if !drop(e) {
			prev, e = e, next
			continue
		}

		if prev == nil && !p.head.CompareAndSwap(e, next) {
			// Pushed onto since: e's new predecessor is above top.
			for prev = p.head.Load(); prev.next.Load() != e; prev = prev.next.Load() {
			}
		}
		if prev != nil {
			prev.next.Store(next)
		}

		p.len.Add(-1)
		dropped = append(dropped, e)
		e = next
	}

	return dropped
}
