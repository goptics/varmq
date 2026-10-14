package varmq

import (
	"fmt"
	"sync"
	"testing"

	"github.com/goptics/varmq/internal/queues"
	"github.com/goptics/varmq/internal/topics"
	"github.com/stretchr/testify/assert"
)

// ackTestQueue is an in-memory queue that hands out ack ids and records
// acknowledgements. It stands in for a persistent adapter.
type ackTestQueue struct {
	*queues.Queue[any]
	mu    sync.Mutex
	n     int
	acked []string
	// emptyAck makes DequeueWithAckId return "" as the ack id.
	emptyAck bool
}

func newAckTestQueue() *ackTestQueue {
	return &ackTestQueue{Queue: queues.NewQueue[any]()}
}

func (q *ackTestQueue) DequeueWithAckId() (any, bool, string) {
	v, ok := q.Dequeue()
	if !ok {
		return nil, false, ""
	}
	if q.emptyAck {
		return v, true, ""
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.n++
	return v, true, fmt.Sprintf("ack-%d", q.n)
}

func (q *ackTestQueue) Acknowledge(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.acked = append(q.acked, id)
	return true
}

func (q *ackTestQueue) ackedIDs() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.acked...)
}

func TestQueueSource(t *testing.T) {
	t.Run("ready job passes through", func(t *testing.T) {
		q := queues.NewQueue[any]()
		q.Enqueue(newJob("x", jobConfigs{Id: "job-1"}))
		src := &queueSource[string, iJob[string]]{q: q}

		assert.Equal(t, 1, src.Len())

		j, err := src.Next()
		assert.NoError(t, err)
		assert.Equal(t, "job-1", j.ID())
		assert.Equal(t, "x", j.Data())
		assert.Equal(t, 0, src.Len())
	})

	t.Run("bytes are parsed into a job", func(t *testing.T) {
		b, err := newJob("payload", jobConfigs{Id: "bytes-1"}).Json()
		assert.NoError(t, err)

		q := queues.NewQueue[any]()
		q.Enqueue(b)
		src := &queueSource[string, iJob[string]]{q: q}

		j, err := src.Next()
		assert.NoError(t, err)
		assert.Equal(t, "bytes-1", j.ID())
		assert.Equal(t, "payload", j.Data())
	})

	t.Run("invalid bytes report ErrParseJob", func(t *testing.T) {
		q := queues.NewQueue[any]()
		q.Enqueue([]byte("not json"))
		src := &queueSource[string, iJob[string]]{q: q}

		_, err := src.Next()
		assert.ErrorIs(t, err, ErrParseJob)
	})

	t.Run("wrong type reports ErrFailedToCastJob", func(t *testing.T) {
		q := queues.NewQueue[any]()
		q.Enqueue(123)
		src := &queueSource[string, iJob[string]]{q: q}

		_, err := src.Next()
		assert.ErrorIs(t, err, ErrFailedToCastJob)
	})

	t.Run("empty queue reports ErrFailedToDequeue", func(t *testing.T) {
		src := &queueSource[string, iJob[string]]{q: queues.NewQueue[any]()}

		_, err := src.Next()
		assert.ErrorIs(t, err, ErrFailedToDequeue)
	})
}

func TestAckSource(t *testing.T) {
	t.Run("bytes get ack id and acknowledge on close", func(t *testing.T) {
		b, err := newJob("payload", jobConfigs{Id: "b-1"}).Json()
		assert.NoError(t, err)

		q := newAckTestQueue()
		q.Enqueue(b)
		src := &ackSource[string, iJob[string]]{q: q}

		assert.Equal(t, 1, src.Len())

		j, err := src.Next()
		assert.NoError(t, err)
		assert.Equal(t, "b-1", j.ID())
		assert.Equal(t, "ack-1", j.(*job[string]).ackId)

		assert.NoError(t, j.Close())
		assert.Equal(t, []string{"ack-1"}, q.ackedIDs())
	})

	t.Run("ready job through ack source acknowledges on close", func(t *testing.T) {
		q := newAckTestQueue()
		q.Enqueue(newJob("x", jobConfigs{Id: "r-1"}))
		src := &ackSource[string, iJob[string]]{q: q}

		j, err := src.Next()
		assert.NoError(t, err)
		assert.Equal(t, "ack-1", j.(*job[string]).ackId)

		assert.NoError(t, j.Close())
		assert.Equal(t, []string{"ack-1"}, q.ackedIDs(), "ready jobs from an ack source must ack too")
	})

	t.Run("empty ack id runs without acking", func(t *testing.T) {
		q := newAckTestQueue()
		q.emptyAck = true
		q.Enqueue(newJob("x", jobConfigs{Id: "e-1"}))
		src := &ackSource[string, iJob[string]]{q: q}

		j, err := src.Next()
		assert.NoError(t, err)
		assert.Equal(t, "", j.(*job[string]).ackId)

		assert.NoError(t, j.Close())
		assert.Empty(t, q.ackedIDs())
	})

	t.Run("wrong type reports ErrFailedToCastJob", func(t *testing.T) {
		q := newAckTestQueue()
		q.Enqueue(3.14)
		src := &ackSource[string, iJob[string]]{q: q}

		_, err := src.Next()
		assert.ErrorIs(t, err, ErrFailedToCastJob)
	})

	t.Run("empty queue reports ErrFailedToDequeue", func(t *testing.T) {
		src := &ackSource[string, iJob[string]]{q: newAckTestQueue()}

		_, err := src.Next()
		assert.ErrorIs(t, err, ErrFailedToDequeue)
	})
}

func TestTopicSource(t *testing.T) {
	newTestWorker := func() *worker[string, iJob[string]] {
		return newWorker(func(j iJob[string]) {}, WithAutoRun(false), WithJobIdGenerator(func() string { return "gen-id" }))
	}

	t.Run("wraps payload in a job with the worker's id generator", func(t *testing.T) {
		log := topics.NewLog[string]()
		cursor := log.NewCursor()
		assert.Equal(t, 1, log.Append("hello"))

		src := &topicSource[string]{cursor: cursor, w: newTestWorker()}

		assert.Equal(t, 1, src.Len())

		j, err := src.Next()
		assert.NoError(t, err)
		assert.Equal(t, "hello", j.Data())
		assert.Equal(t, "gen-id", j.ID())
		assert.Equal(t, 0, src.Len())
	})

	t.Run("empty cursor reports ErrFailedToDequeue", func(t *testing.T) {
		log := topics.NewLog[string]()
		src := &topicSource[string]{cursor: log.NewCursor(), w: newTestWorker()}

		_, err := src.Next()
		assert.ErrorIs(t, err, ErrFailedToDequeue)
	})

	t.Run("closed cursor reports ErrFailedToDequeue", func(t *testing.T) {
		log := topics.NewLog[string]()
		cursor := log.NewCursor()
		log.Append("hello")
		cursor.Close()

		src := &topicSource[string]{cursor: cursor, w: newTestWorker()}

		assert.Equal(t, 0, src.Len())
		_, err := src.Next()
		assert.ErrorIs(t, err, ErrFailedToDequeue)
	})
}
