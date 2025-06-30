package pool

import (
	"sync"

	"github.com/goptics/varmq/internal/linkedlist"
)

type Pool[T any] struct {
	*linkedlist.List[Node[T]]
	Cache sync.Pool
}

func New[T any](cap int) *Pool[T] {
	return &Pool[T]{
		List: linkedlist.New[Node[T]](),
		Cache: sync.Pool{
			New: func() any {
				return linkedlist.NewNode(CreateNode[T](cap))
			},
		},
	}
}
