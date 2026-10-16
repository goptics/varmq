package queues

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// An Enqueue that is already waiting for the queue lock when Close runs must
// observe the closed flag, otherwise it appends to a queue nobody drains. The
// FIFO Queue gets this from topics.Log; see its "append waiting for the lock"
// test.
func TestEnqueueRejectedWhenClosedWhileWaitingForLock(t *testing.T) {
	t.Run("PriorityQueue", func(t *testing.T) {
		q := NewPriorityQueue[int]()
		q.mx.Lock()

		res := make(chan bool, 1)
		go func() { res <- q.Enqueue(1, 0) }()

		time.Sleep(50 * time.Millisecond)
		q.Close()
		q.mx.Unlock()

		assert.False(t, <-res)
		assert.Equal(t, 0, q.Len())
	})
}
