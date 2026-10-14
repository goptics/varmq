package varmq

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

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
	// failAck makes Acknowledge report failure.
	failAck bool
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
	return !q.failAck
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

func TestBind(t *testing.T) {
	t.Run("custom acknowledgeable queue bound through newQueue acks on completion", func(t *testing.T) {
		aq := newAckTestQueue()
		w := newWorker(func(j iJob[string]) {}, WithAutoRun(false))
		q := newQueue(w, aq)

		job, ok := q.Add("x")
		assert.True(t, ok)

		assert.NoError(t, w.Start())
		defer w.Stop()
		job.Wait()

		assert.Equal(t, []string{"ack-1"}, aq.ackedIDs())
	})

	t.Run("plain queue binds as queueSource, ack queue as ackSource", func(t *testing.T) {
		w := newWorker(func(j iJob[string]) {}, WithAutoRun(false))

		plain := w.bind(queues.NewQueue[any](), 1)
		_, isQueue := plain.(*queueSource[string, iJob[string]])
		assert.True(t, isQueue)

		acked := w.bind(newAckTestQueue(), 1)
		_, isAck := acked.(*ackSource[string, iJob[string]])
		assert.True(t, isAck)

		assert.Equal(t, 2, w.numSources())
	})

	t.Run("closing a queued job while the ack source dequeues it does not race", func(t *testing.T) {
		// Run under -race: the user closes job B while the scheduler is about
		// to pull it from an acknowledgeable queue. The ack source must not
		// write the ack id onto a job the user already closed.
		//
		// A separate goroutine unblocks job A after a short sleep, so B is
		// dispatched with no happens-before edge to the user's Close other than
		// the job's status. The head start keeps the user's Close clear of the
		// scheduler's IsClosed check: a Close that interleaves with that check
		// is a job lifecycle race outside the source layer.
		for i := 0; i < 30; i++ {
			started := make(chan struct{})
			unblock := make(chan struct{})
			releaseB := make(chan struct{})

			w := newWorker(func(j iJob[string]) {
				switch j.Data() {
				case "a":
					close(started)
					<-unblock
				case "b":
					<-releaseB
				}
			}, WithAutoRun(false), WithConcurrency(1))
			q := newQueue(w, newAckTestQueue())
			assert.NoError(t, w.Start())

			jA, ok := q.Add("a")
			assert.True(t, ok)
			<-started

			jB, ok := q.Add("b")
			assert.True(t, ok)

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(2 * time.Millisecond)
				close(unblock)
			}()
			_ = jB.Close() // nil, or ErrJobProcessing if the scheduler won
			wg.Wait()
			close(releaseB)

			jA.Wait()
			w.Wait()

			assert.Contains(t, []string{"Closed", "Finished"}, jB.Status(), "iteration %d", i)
			assert.NoError(t, w.Stop())
		}
	})

	t.Run("failed acknowledgement of a ready job leaves it finished and reports the error", func(t *testing.T) {
		aq := newAckTestQueue()
		aq.failAck = true
		w := newWorker(func(j iJob[string]) {}, WithAutoRun(false))
		q := newQueue(w, aq)

		job, ok := q.Add("x")
		assert.True(t, ok)

		assert.NoError(t, w.Start())
		defer w.Stop()
		w.Wait()

		assert.Equal(t, "Finished", job.Status())

		done := make(chan struct{})
		go func() {
			job.Wait()
			close(done)
		}()
		select {
		case <-done:
			t.Fatal("job.Wait returned although its acknowledgement failed")
		case <-time.After(50 * time.Millisecond):
		}

		select {
		case err := <-w.Errs():
			assert.True(t, errors.Is(err, ErrAcknowledgeJob), "got %v", err)
		case <-time.After(time.Second):
			t.Fatal("no ErrAcknowledgeJob on the worker's error channel")
		}
	})
}

func TestWorkerSources(t *testing.T) {
	newTestWorker := func() *worker[string, iJob[string]] {
		return newWorker(func(j iJob[string]) {}, WithAutoRun(false))
	}

	t.Run("fresh worker has no source", func(t *testing.T) {
		w := newTestWorker()

		_, ok := w.currentSource()
		assert.False(t, ok)
		assert.Equal(t, 0, w.numSources())
		assert.Equal(t, 0, w.NumPending())
	})

	t.Run("single bind attaches the adapter directly, no manager", func(t *testing.T) {
		w := newTestWorker()
		q := queues.NewQueue[any]()
		q.Enqueue(newJob("x", jobConfigs{Id: "x"}))

		src := w.bind(q, 1)

		cur, ok := w.currentSource()
		assert.True(t, ok)
		assert.Same(t, src, cur)
		_, isManager := cur.(*sourceManager[iJob[string]])
		assert.False(t, isManager, "one source must not be wrapped in a manager")
		assert.Equal(t, 1, w.numSources())
		assert.Equal(t, 1, w.NumPending())
	})

	t.Run("second bind promotes to a manager and keeps the first priority", func(t *testing.T) {
		w := newTestWorker()
		q1 := queues.NewQueue[any]()
		q2 := queues.NewQueue[any]()
		q1.Enqueue(newJob("one", jobConfigs{Id: "one"}))
		q2.Enqueue(newJob("two", jobConfigs{Id: "two"}))

		w.bind(q1, 5)
		s2 := w.bind(q2, 1)

		cur, _ := w.currentSource()
		m, ok := cur.(*sourceManager[iJob[string]])
		assert.True(t, ok, "two sources live behind a manager")
		assert.Equal(t, 2, m.Count())
		assert.Equal(t, 2, w.numSources())
		assert.Equal(t, 2, w.NumPending())

		picked, found := m.next()
		assert.True(t, found)
		assert.Same(t, s2, picked, "priority 1 beats the first source's priority 5")

		q3 := queues.NewQueue[any]()
		w.bind(q3, 3)

		cur2, _ := w.currentSource()
		assert.Same(t, m, cur2, "third bind reuses the same manager")
		assert.Equal(t, 3, m.Count())
	})

	t.Run("detach clears a single source and drops one from a manager", func(t *testing.T) {
		w := newTestWorker()
		s := w.bind(queues.NewQueue[any](), 1)
		w.detach(s)
		assert.Equal(t, 0, w.numSources())
		_, ok := w.currentSource()
		assert.False(t, ok)

		a := w.bind(queues.NewQueue[any](), 1)
		w.bind(queues.NewQueue[any](), 2)
		w.detach(a)
		assert.Equal(t, 1, w.numSources())
		cur, _ := w.currentSource()
		_, isManager := cur.(*sourceManager[iJob[string]])
		assert.True(t, isManager, "manager stays once created")
	})

	t.Run("manager Next pulls from the chosen source or reports nothing ready", func(t *testing.T) {
		m := newSourceManager[iJob[string]](Priority)
		m.Register(newTestSource(), 1)

		_, err := m.Next()
		assert.ErrorIs(t, err, errNoReadySource)

		m.Register(newTestSource("a"), 2)

		j, err := m.Next()
		assert.NoError(t, err)
		assert.Equal(t, "a", j.ID())
		assert.Equal(t, 0, m.Len())
	})

	t.Run("topic-only worker pulls straight from its topicSource", func(t *testing.T) {
		topic := NewTopic[int]()
		wb := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		wb.WithTopic(topic)

		w := wb.(*workerBinder[int]).worker
		cur, ok := w.currentSource()
		assert.True(t, ok)
		_, isTopic := cur.(*topicSource[int])
		assert.True(t, isTopic, "no manager between the scheduler and the topic")
	})
}
