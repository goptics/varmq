package varmq

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goptics/varmq/internal/queues"
	"github.com/goptics/varmq/internal/topics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		assert.Equal(t, "ack-1", j.(*job[string]).ackRef.Load().id)

		assert.NoError(t, j.Close())
		assert.Equal(t, []string{"ack-1"}, q.ackedIDs())
	})

	t.Run("ready job through ack source acknowledges on close", func(t *testing.T) {
		q := newAckTestQueue()
		q.Enqueue(newJob("x", jobConfigs{Id: "r-1"}))
		src := &ackSource[string, iJob[string]]{q: q}

		j, err := src.Next()
		assert.NoError(t, err)
		assert.Equal(t, "ack-1", j.(*job[string]).ackRef.Load().id)

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
		assert.Nil(t, j.(*job[string]).ackRef.Load(), "no ack id means nothing to acknowledge")

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

		assert.Equal(t, 2, w.manager().count())
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

	t.Run("fresh worker has an emptyManager", func(t *testing.T) {
		w := newTestWorker()

		_, isEmpty := w.manager().(*emptyManager[iJob[string]])
		assert.True(t, isEmpty)
		assert.Equal(t, 0, w.manager().count())
		assert.Equal(t, 0, w.NumPending())

		_, err := w.manager().Next()
		assert.ErrorIs(t, err, errNoReadySource)
	})

	t.Run("one source is pulled directly, without a strategy", func(t *testing.T) {
		w := newTestWorker()
		q := queues.NewQueue[any]()
		q.Enqueue(newJob("x", jobConfigs{Id: "x"}))

		src := w.bind(q, 1)

		d, isDirect := w.manager().(*directManager[iJob[string]])
		assert.True(t, isDirect)
		assert.Same(t, src, d.src)
		assert.Equal(t, 1, w.manager().count())
		assert.Equal(t, 1, w.NumPending())

		j, err := w.manager().Next()
		assert.NoError(t, err)
		assert.Equal(t, "x", j.ID())

		_, err = w.manager().Next()
		assert.ErrorIs(t, err, errNoReadySource, "an empty direct source is nothing to do, not a failure")
	})

	t.Run("a second source switches to a strategyManager", func(t *testing.T) {
		w := newTestWorker()
		q1 := queues.NewQueue[any]()
		q2 := queues.NewQueue[any]()
		q1.Enqueue(newJob("one", jobConfigs{Id: "one"}))
		q2.Enqueue(newJob("two", jobConfigs{Id: "two"}))

		w.bind(q1, 5)
		s2 := w.bind(q2, 1)

		m, isStrategy := w.manager().(*strategyManager[iJob[string]])
		assert.True(t, isStrategy)
		assert.Equal(t, 2, m.count())
		assert.Equal(t, 2, w.NumPending())

		picked, found := m.next()
		assert.True(t, found)
		assert.Same(t, s2, picked, "priority 1 beats the first source's priority 5")

		w.bind(queues.NewQueue[any](), 3)
		assert.Same(t, m, w.manager(), "a third source joins the same strategyManager")
		assert.Equal(t, 3, w.manager().count())
	})

	t.Run("removing sources steps back down to direct, then empty", func(t *testing.T) {
		w := newTestWorker()
		a := w.bind(queues.NewQueue[any](), 5)
		b := w.bind(queues.NewQueue[any](), 1)

		w.removeSource(b)
		d, isDirect := w.manager().(*directManager[iJob[string]])
		assert.True(t, isDirect, "one source left: no strategy")
		assert.Same(t, a, d.src)
		assert.Equal(t, 5, d.priority, "the remaining source keeps its priority")

		w.removeSource(a)
		_, isEmpty := w.manager().(*emptyManager[iJob[string]])
		assert.True(t, isEmpty)
		assert.Equal(t, 0, w.NumPending())
	})

	t.Run("a source kept through direct keeps its rank when another joins", func(t *testing.T) {
		w := newTestWorker()
		qa := queues.NewQueue[any]()
		qc := queues.NewQueue[any]()
		qa.Enqueue(newJob("a", jobConfigs{Id: "a"}))
		qc.Enqueue(newJob("c", jobConfigs{Id: "c"}))

		w.bind(qa, 5)
		b := w.bind(queues.NewQueue[any](), 1)
		w.removeSource(b)
		sc := w.bind(qc, 3)

		m, isStrategy := w.manager().(*strategyManager[iJob[string]])
		assert.True(t, isStrategy)
		picked, found := m.next()
		assert.True(t, found)
		assert.Same(t, sc, picked, "priority 3 beats the kept source's priority 5")
	})

	t.Run("removing an unknown source changes nothing", func(t *testing.T) {
		w := newTestWorker()
		a := w.bind(queues.NewQueue[any](), 1)
		other := &queueSource[string, iJob[string]]{q: queues.NewQueue[any]()}

		w.removeSource(other)

		d, isDirect := w.manager().(*directManager[iJob[string]])
		assert.True(t, isDirect)
		assert.Same(t, a, d.src)
	})

	t.Run("manager Next pulls from the chosen source or reports nothing ready", func(t *testing.T) {
		m := newStrategyManager[iJob[string]](Priority)
		m.Register(newTestSource(), 1)

		_, err := m.Next()
		assert.ErrorIs(t, err, errNoReadySource)

		m.Register(newTestSource("a"), 2)

		j, err := m.Next()
		assert.NoError(t, err)
		assert.Equal(t, "a", j.ID())
		assert.Equal(t, 0, m.Len())
	})

	t.Run("a topic-only worker pulls its topicSource directly", func(t *testing.T) {
		topic := NewTopic[int]()
		wb := NewWorker(func(j Job[int]) {}, WithAutoRun(false))
		wb.WithTopic(topic)

		w := wb.(*workerBinder[int]).worker
		d, isDirect := w.manager().(*directManager[iJob[int]])
		assert.True(t, isDirect, "no strategy between the scheduler and the topic")
		_, isTopic := d.src.(*topicSource[int])
		assert.True(t, isTopic)
	})
}

func TestMaxBatch(t *testing.T) {
	newTestWorker := func() *worker[string, iJob[string]] {
		return newWorker(func(j iJob[string]) {}, WithAutoRun(false))
	}

	t.Run("per source kind", func(t *testing.T) {
		w := newTestWorker()
		assert.Equal(t, 1, w.manager().maxBatch(), "nothing bound")

		fifo := w.bind(queues.NewQueue[any](), 1)
		assert.Equal(t, maxBatch, fifo.maxBatch(), "in-memory FIFO batches")
		assert.Equal(t, maxBatch, w.manager().maxBatch(), "direct manager forwards its source")

		assert.Equal(t, 1, newTestWorker().bind(queues.NewPriorityQueue[any](), 1).maxBatch(), "priority storage does not batch")
		assert.Equal(t, 1, newTestWorker().bind(newAckTestQueue(), 1).maxBatch(), "acknowledging storage does not batch")

		topic := &topicSource[string]{cursor: topics.NewLog[string]().NewCursor(), w: newTestWorker()}
		assert.Equal(t, maxBatch, topic.maxBatch(), "in-memory topic batches")

		w.bind(queues.NewQueue[any](), 2)
		assert.Equal(t, 1, w.manager().maxBatch(), "several sources: the strategy picks per job")
	})

	t.Run("dispatcher takes one job at a time from a priority queue", func(t *testing.T) {
		fifo, prio := queues.NewQueue[any](), queues.NewPriorityQueue[any]()
		for _, tc := range []struct {
			name        string
			q           IBaseQueue
			enqueue     func(any)
			leftInQueue int
		}{
			{"fifo", fifo, func(j any) { fifo.Enqueue(j) }, 0},
			{"priority", prio, func(j any) { prio.Enqueue(j, 1) }, 4},
		} {
			t.Run(tc.name, func(t *testing.T) {
				block := make(chan struct{})
				w := newWorker(func(j iJob[string]) { <-block }, WithAutoRun(false), WithConcurrency(1))
				w.jobNanos.Store(1) // measured: cheap jobs batch
				defer close(block)
				defer w.Stop()

				for range 5 {
					tc.enqueue(newJob("x", loadJobConfigs(w.configs())))
				}
				w.bind(tc.q, 1)

				w.status.Store(idle) // as Start leaves it: dispatch runs only on an active worker
				w.processNextJob()
				assert.Equal(t, tc.leftInQueue, tc.q.Len(), "jobs left in storage")
				assert.Equal(t, 4, w.NumPending(), "not started, in storage or in a batch")
			})
		}
	})

	t.Run("buffered jobs are pending, not processing, until they run", func(t *testing.T) {
		block := make(chan struct{})
		// Auto-run off so all five are queued before the first dispatch; with
		// it on, the loop may hand out job 0 before the other Adds land.
		w := NewWorker(func(j Job[int]) { <-block }, 1, WithAutoRun(false))
		w.(*workerBinder[int]).jobNanos.Store(1) // measured: cheap jobs batch
		defer close(block)
		defer w.Stop()
		q := w.BindQueue()

		var jobs []EnqueuedJob
		for i := range 5 {
			j, _ := q.Add(i)
			jobs = append(jobs, j)
		}
		assert.NoError(t, w.Start())

		// The worker marks job 0 Processing only after it has received the
		// whole batch; the busy count alone rises before the dispatcher has
		// finished dequeuing.
		assert.Eventually(t, func() bool { return jobs[0].Status() == "Processing" }, time.Second, time.Millisecond)

		assert.Equal(t, 0, q.Len(), "the whole backlog went to the one worker as a batch")
		assert.Equal(t, 4, w.NumPending())
		assert.Equal(t, 1, w.NumProcessing())
		m := w.Metrics()
		assert.Equal(t, int(m.Submitted()), w.NumPending()+w.NumProcessing()+int(m.Completed()))
		assert.Equal(t, "Processing", jobs[0].Status())
		assert.Equal(t, "Queued", jobs[1].Status(), "waiting in the batch")

		assert.NoError(t, jobs[1].Close(), "a job waiting in a batch can still be closed")
	})
}

// batchTestQueue is an in-memory persistent adapter that also pops in
// batches, counting the storage calls the worker makes.
type batchTestQueue struct {
	mu        sync.Mutex
	items     []any
	n         int
	lens      atomic.Int64
	pops      atomic.Int64
	maxWant   atomic.Int64           // the largest count a pop asked for
	fail      atomic.Bool            // pops fail
	closed    atomic.Bool            // like a closed adapter: Len is 0, Enqueue and pops fail
	duringPop atomic.Pointer[func()] // runs once inside the next pop, after it took its items
}

func (q *batchTestQueue) Len() int {
	q.lens.Add(1)
	if q.closed.Load() {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// push stores v behind the worker's back: no poke, no signal.
func (q *batchTestQueue) push(v any) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, v)
}

func (q *batchTestQueue) Enqueue(v any) bool {
	if q.closed.Load() {
		return false
	}
	q.push(v)
	return true
}

func (q *batchTestQueue) Dequeue() (any, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil, false
	}
	v := q.items[0]
	q.items = q.items[1:]
	return v, true
}

func (q *batchTestQueue) Values() []any {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]any(nil), q.items...)
}

func (q *batchTestQueue) Purge() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = nil
}

func (q *batchTestQueue) Close() error            { return nil }
func (q *batchTestQueue) Acknowledge(string) bool { return true }

func (q *batchTestQueue) DequeueWithAckId() (any, bool, string) {
	v, ok := q.Dequeue()
	if !ok {
		return nil, false, ""
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.n++
	return v, true, fmt.Sprintf("ack-%d", q.n)
}

func (q *batchTestQueue) DequeueBatchWithAckIds(items []any, ackIDs []string) (int, int, error) {
	q.pops.Add(1)
	for want := int64(min(len(items), len(ackIDs))); ; {
		m := q.maxWant.Load()
		if want <= m || q.maxWant.CompareAndSwap(m, want) {
			break
		}
	}
	if q.fail.Load() || q.closed.Load() {
		return 0, 0, errors.New("pop failed")
	}

	q.mu.Lock()
	n := min(len(items), len(ackIDs), len(q.items))
	for i := range n {
		q.n++
		items[i], ackIDs[i] = q.items[i], fmt.Sprintf("ack-%d", q.n)
	}
	q.items = q.items[n:]
	remaining := len(q.items)
	q.mu.Unlock()

	if f := q.duringPop.Swap(nil); f != nil {
		(*f)()
	}

	return n, remaining, nil
}

// batchTestDistQueue is the distributed variant: every enqueue announces
// itself to the subscribers.
type batchTestDistQueue struct {
	*batchTestQueue
	subMu sync.Mutex
	subs  []func(string)
}

func (q *batchTestDistQueue) Subscribe(fn func(string)) {
	q.subMu.Lock()
	defer q.subMu.Unlock()
	q.subs = append(q.subs, fn)
}

func (q *batchTestDistQueue) announce() {
	q.subMu.Lock()
	subs := slices.Clone(q.subs)
	q.subMu.Unlock()
	for _, fn := range subs {
		fn("enqueued")
	}
}

func (q *batchTestDistQueue) Enqueue(v any) bool {
	ok := q.batchTestQueue.Enqueue(v)
	if ok {
		q.announce()
	}
	return ok
}

type batchTestPrioQueue struct{ *batchTestQueue }

type batchTestDistPrioQueue struct{ *batchTestDistQueue }

func (q *batchTestDistPrioQueue) Enqueue(v any, _ int) bool { return q.batchTestDistQueue.Enqueue(v) }

func (q *batchTestPrioQueue) Enqueue(v any, _ int) bool { return q.batchTestQueue.Enqueue(v) }

var (
	_ IPersistentQueue           = (*batchTestQueue)(nil)
	_ IBatchAcknowledgeable      = (*batchTestQueue)(nil)
	_ IDistributedQueue          = (*batchTestDistQueue)(nil)
	_ IPersistentPriorityQueue   = (*batchTestPrioQueue)(nil)
	_ creditSource[iJob[string]] = (*batchAckSource[string, iJob[string]])(nil)
)

func jobBytes(t *testing.T, v string) []byte {
	t.Helper()
	b, err := newJob(v, loadJobConfigs(newConfig())).Json()
	require.NoError(t, err)
	return b
}

// recorder is a worker function that records what it ran.
type recorder struct {
	mu   sync.Mutex
	done []string
}

func (r *recorder) run(j Job[string]) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done = append(r.done, j.Data())
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.done)
}

func (r *recorder) has(v string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.done {
		if d == v {
			return true
		}
	}
	return false
}

func internalWorker(wb IWorkerBinder[string]) *worker[string, iJob[string]] {
	return wb.(*workerBinder[string]).worker
}

// waitPops waits for the pop bind triggers, so the hint is known dry.
func waitPops(t *testing.T, q *batchTestQueue, n int64) {
	t.Helper()
	require.Eventually(t, func() bool { return q.pops.Load() >= n }, time.Second, time.Millisecond)
	time.Sleep(10 * time.Millisecond) // let the loop finish the pass
}

func TestBatchAckSourceHint(t *testing.T) {
	newSrc := func(q ackQueue, bq IBatchAcknowledgeable) *batchAckSource[string, iJob[string]] {
		return newBatchAckSource(q, bq, newWorker(func(iJob[string]) {}, WithAutoRun(false))) // never active: a resync stops at once
	}
	fetch := func(t *testing.T, s *batchAckSource[string, iJob[string]], want int) (jobs []iJob[string], errs []error) {
		t.Helper()
		require.NoError(t, s.fetch(want, func(j iJob[string]) { jobs = append(jobs, j) }, func(err error) { errs = append(errs, err) }))
		return jobs, errs
	}

	t.Run("persistent", func(t *testing.T) {
		q := &batchTestQueue{}
		s := newSrc(q, q)
		assert.Equal(t, 1, s.ready(), "unknown until the first pop")

		q.push(jobBytes(t, "a"))
		q.push(jobBytes(t, "b"))
		q.push(jobBytes(t, "c"))
		jobs, _ := fetch(t, s, 1)
		require.Len(t, jobs, 1)
		assert.Equal(t, "a", jobs[0].Data())
		assert.Equal(t, 2, s.ready(), "the remaining count")

		jobs, _ = fetch(t, s, 2)
		assert.Len(t, jobs, 2)
		assert.Equal(t, 0, s.ready(), "a full pop that emptied a persistent queue")

		s.poke()
		assert.Equal(t, 1, s.ready())
		jobs, _ = fetch(t, s, 1)
		assert.Empty(t, jobs)
		assert.Equal(t, 0, s.ready(), "a short pop")
	})

	t.Run("distributed full pop probes again", func(t *testing.T) {
		q := &batchTestDistQueue{batchTestQueue: &batchTestQueue{}}
		s := newSrc(q, q)
		s.subscribed.Store(true)
		q.push(jobBytes(t, "a"))
		fetch(t, s, 1)
		assert.Equal(t, 1, s.ready(), "the consumer did not join the idle set")
		fetch(t, s, 1)
		assert.Equal(t, 0, s.ready(), "a short pop")
	})

	t.Run("poke during the pop is kept", func(t *testing.T) {
		q := &batchTestQueue{}
		s := newSrc(q, q)
		fetch(t, s, 1)
		require.Equal(t, 0, s.ready())

		s.poke()
		during := func() { q.push(jobBytes(t, "late")); s.poke() }
		q.duringPop.Store(&during)
		jobs, _ := fetch(t, s, 1)
		assert.Empty(t, jobs, "the pop ran before the item landed")
		assert.Equal(t, 1, s.ready(), "the poke that landed during the pop")
		jobs, _ = fetch(t, s, 1)
		require.Len(t, jobs, 1)
		assert.Equal(t, "late", jobs[0].Data())
	})

	t.Run("short pop with items left", func(t *testing.T) {
		q := &batchTestQueue{}
		s := newSrc(q, q)
		q.push(jobBytes(t, "a"))
		q.push(jobBytes(t, "b"))
		q.push(jobBytes(t, "c"))
		during := func() { q.push(jobBytes(t, "d")) }
		q.duringPop.Store(&during)
		s.fetch(5, func(iJob[string]) {}, func(error) {})
		assert.Equal(t, 0, s.ready(), "pop came back short and left nothing at the time")

		// A storage handing back a failed call's items returns a short pop
		// with items left; the count wins over the dry mark.
		s.remaining.Store(4)
		assert.Equal(t, 4, s.ready())
	})

	t.Run("error after a pop that left items", func(t *testing.T) {
		q := &batchTestQueue{}
		s := newSrc(q, q)
		q.push(jobBytes(t, "a"))
		q.push(jobBytes(t, "b"))
		q.push(jobBytes(t, "c"))
		fetch(t, s, 1)
		require.Equal(t, 2, s.ready())
		q.fail.Store(true)
		assert.Error(t, s.fetch(1, func(iJob[string]) {}, func(error) {}))
		assert.Equal(t, 0, s.ready(), "a failed pop forgets the count")
	})

	t.Run("empty pop with items left", func(t *testing.T) {
		q := &heldQueue{batchTestQueue: &batchTestQueue{}}
		s := newSrc(q, q)
		q.push(jobBytes(t, "a"))
		q.hold.Store(true)
		fetch(t, s, 1)
		assert.Equal(t, 0, s.ready(), "nothing poppable now: wait for a poke, do not spin")
	})

	t.Run("distributed full pop clears a dry mark", func(t *testing.T) {
		q := &shortOnceQueue{batchTestDistQueue: &batchTestDistQueue{batchTestQueue: &batchTestQueue{}}}
		s := newSrc(q, q)
		s.subscribed.Store(true)
		q.push(jobBytes(t, "a"))
		q.push(jobBytes(t, "b"))
		jobs, _ := fetch(t, s, 2)
		require.Len(t, jobs, 1, "a short pop with an item left")
		require.Equal(t, 1, s.ready())
		jobs, _ = fetch(t, s, 1)
		require.Len(t, jobs, 1)
		assert.Equal(t, 1, s.ready(), "a full pop that emptied a distributed queue probes once more")
	})

	t.Run("error marks dry", func(t *testing.T) {
		q := &batchTestQueue{}
		s := newSrc(q, q)
		q.push(jobBytes(t, "a"))
		q.fail.Store(true)
		assert.Error(t, s.fetch(1, func(iJob[string]) {}, func(error) {}))
		assert.Equal(t, 0, s.ready())
	})

	t.Run("bad and closed items", func(t *testing.T) {
		q := &batchTestQueue{}
		s := newSrc(q, q)
		closed := newJob("closed", loadJobConfigs(newConfig()))
		closed.Close()
		q.push([]byte("not json"))
		q.push(closed)
		q.push(jobBytes(t, "ok"))
		jobs, errs := fetch(t, s, 3)
		require.Len(t, jobs, 1)
		assert.Equal(t, "ok", jobs[0].Data())
		assert.Len(t, errs, 1, "the unparsable item is reported")
		assert.Nil(t, closed.ackRef.Load(), "a closed job is not mutated")
		assert.NotNil(t, jobs[0].(*job[string]).ackRef.Load(), "the job carries its ack id")
	})
}

// The scheduler reads the hint, not the storage length, between pops.
func TestBatchSourceNoLenProbes(t *testing.T) {
	q := &batchTestQueue{}
	var wg sync.WaitGroup
	wb := NewWorker(func(Job[string]) { wg.Done() }, WithConcurrency(4))
	defer wb.Stop()
	pq := wb.WithPersistentQueue(q)
	waitPops(t, q, 1)

	const n = 500
	wg.Add(n)
	for i := range n {
		require.True(t, pq.Add(fmt.Sprint(i)))
	}
	wg.Wait()

	assert.LessOrEqual(t, q.lens.Load(), int64(2), "Len is for Wait, strategies and the resync only")
	assert.LessOrEqual(t, q.pops.Load(), int64(n+2), "at most about one pop per job")
}

// An Add followed by Wait waits for that job, through each binding.
func TestBatchSourceAddThenWait(t *testing.T) {
	cases := []struct {
		name string
		bind func(wb IWorkerBinder[string]) (*batchTestQueue, func(string) bool)
	}{
		{"persistent", func(wb IWorkerBinder[string]) (*batchTestQueue, func(string) bool) {
			q := &batchTestQueue{}
			pq := wb.WithPersistentQueue(q)
			return q, func(s string) bool { return pq.Add(s) }
		}},
		{"persistent priority", func(wb IWorkerBinder[string]) (*batchTestQueue, func(string) bool) {
			q := &batchTestPrioQueue{&batchTestQueue{}}
			pq := wb.WithPersistentPriorityQueue(q)
			return q.batchTestQueue, func(s string) bool { return pq.Add(s, 1) }
		}},
		{"distributed", func(wb IWorkerBinder[string]) (*batchTestQueue, func(string) bool) {
			q := &batchTestDistQueue{batchTestQueue: &batchTestQueue{}}
			dq := wb.WithDistributedQueue(q)
			return q.batchTestQueue, func(s string) bool { return dq.Add(s) }
		}},
		{"distributed priority", func(wb IWorkerBinder[string]) (*batchTestQueue, func(string) bool) {
			q := &batchTestDistPrioQueue{&batchTestDistQueue{batchTestQueue: &batchTestQueue{}}}
			dq := wb.WithDistributedPriorityQueue(q)
			return q.batchTestQueue, func(s string) bool { return dq.Add(s, 1) }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var r recorder
			wb := NewWorker(r.run, WithConcurrency(2))
			defer wb.Stop()
			q, add := c.bind(wb)
			waitPops(t, q, 1) // the hint is now dry: only a poke brings the next pop

			start := time.Now()
			for i := range 20 {
				require.True(t, add(fmt.Sprint(i)))
				wb.Wait()
				require.Equal(t, i+1, r.count())
			}
			assert.Less(t, time.Since(start), 500*time.Millisecond, "each add pokes; none waits for the resync")
		})
	}
}

// A failed pop is reported once and not retried until there is a new sign
// of work; a persistent source finds the item again through the resync.
func TestBatchSourceErrorNoSpin(t *testing.T) {
	q := &batchTestQueue{}
	var r recorder
	var errs atomic.Int64
	wb := NewWorker(r.run, WithErrHandler(func(Worker, error) { errs.Add(1) }))
	defer wb.Stop()
	pq := wb.WithPersistentQueue(q)
	waitPops(t, q, 1)

	q.fail.Store(true)
	require.True(t, pq.Add("a"))
	time.Sleep(200 * time.Millisecond)
	assert.EqualValues(t, 2, q.pops.Load(), "one pop for the add, no retries")
	assert.EqualValues(t, 1, errs.Load())

	q.fail.Store(false)
	done := make(chan struct{})
	go func() { wb.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not return after the storage recovered")
	}
	assert.True(t, r.has("a"))
}

// A closed adapter yields one error and nothing more.
func TestBatchSourceClosedNoSpin(t *testing.T) {
	q := &batchTestQueue{}
	q.closed.Store(true)
	var errs atomic.Int64
	wb := NewWorker(func(Job[string]) {}, WithErrHandler(func(Worker, error) { errs.Add(1) }))
	defer wb.Stop()
	wb.WithPersistentQueue(q)

	time.Sleep(1500 * time.Millisecond) // past one resync
	assert.EqualValues(t, 1, q.pops.Load())
	assert.EqualValues(t, 1, errs.Load())
}

// A persistent source finds items no poke announced (a requeue, another
// process) within a resync period, also after a pause.
func TestBatchSourceResync(t *testing.T) {
	q := &batchTestQueue{}
	var r recorder
	wb := NewWorker(r.run)
	defer wb.Stop()
	wb.WithPersistentQueue(q)
	waitPops(t, q, 1)

	time.Sleep(1200 * time.Millisecond) // one resync finds the queue empty and re-arms
	q.push(jobBytes(t, "behind"))
	require.Eventually(t, func() bool { return r.has("behind") }, 1500*time.Millisecond, 10*time.Millisecond)

	require.NoError(t, wb.Pause())
	time.Sleep(1200 * time.Millisecond) // the resync fires while paused and stops
	require.NoError(t, wb.Resume())
	q.push(jobBytes(t, "after resume"))
	require.Eventually(t, func() bool { return r.has("after resume") }, 1500*time.Millisecond, 10*time.Millisecond)
}

// A distributed wake-up pokes the hint.
func TestBatchSourceDistributedWake(t *testing.T) {
	q := &batchTestDistQueue{batchTestQueue: &batchTestQueue{}}
	var r recorder
	wb := NewWorker(r.run)
	defer wb.Stop()
	wb.WithDistributedQueue(q)
	waitPops(t, q.batchTestQueue, 1)

	q.push(jobBytes(t, "a")) // stored by another process
	time.Sleep(50 * time.Millisecond)
	assert.False(t, r.has("a"), "dry until something announces work")
	q.announce()
	require.Eventually(t, func() bool { return r.has("a") }, 500*time.Millisecond, time.Millisecond)
	assert.EqualValues(t, 1, wb.Metrics().Submitted(), "the wake-up still counts")
}

// NumPending is the storage's exact length, not the hint.
func TestBatchSourceNumPendingExact(t *testing.T) {
	q := &batchTestQueue{}
	wb := NewWorker(func(Job[string]) {})
	defer wb.Stop()
	wb.WithPersistentQueue(q)
	waitPops(t, q, 1)
	require.NoError(t, wb.Pause())

	for i := range 5 {
		q.push(jobBytes(t, fmt.Sprint(i)))
	}
	assert.Equal(t, 5, wb.NumPending())
}

// While another source shares the worker the hint is not kept; when the
// batch source is alone again it pops on the next signal, without waiting
// for the resync.
func TestBatchSourceStepDown(t *testing.T) {
	q := &batchTestQueue{}
	var r recorder
	wb := NewWorker(r.run)
	defer wb.Stop()
	w := internalWorker(wb)
	wb.WithPersistentQueue(q)
	waitPops(t, q, 1)

	topic := NewTopic[string]()
	sub := wb.WithTopic(topic)
	_, isStrategy := w.manager().(*strategyManager[iJob[string]])
	require.True(t, isStrategy)
	time.Sleep(20 * time.Millisecond) // the subscribe's wake-up has passed
	q.push(jobBytes(t, "x"))          // no poke, and nothing wakes the worker
	sub.Unsubscribe()
	_, isCredit := w.manager().(*creditManager[iJob[string]])
	require.True(t, isCredit)

	require.Eventually(t, func() bool { return r.has("x") }, 300*time.Millisecond, time.Millisecond, "the step-down signals the worker")
}

// Items stored before the bind are popped at once: bind signals the worker.
func TestBatchSourceBindPopsExisting(t *testing.T) {
	q := &batchTestQueue{}
	q.push(jobBytes(t, "old"))
	var r recorder
	wb := NewWorker(r.run)
	defer wb.Stop()
	time.Sleep(20 * time.Millisecond) // the worker's start signal is long consumed
	wb.WithPersistentQueue(q)
	require.Eventually(t, func() bool { return r.has("old") }, 500*time.Millisecond, time.Millisecond)
}

// A job the user closed is skipped, and the pop goes on to the next one.
func TestBatchSourceSkipsClosedJob(t *testing.T) {
	q := &batchTestQueue{}
	closed := newJob("closed", loadJobConfigs(newConfig()))
	closed.Close()
	q.push(closed)
	q.push(jobBytes(t, "next"))
	var r recorder
	wb := NewWorker(r.run)
	defer wb.Stop()
	wb.WithPersistentQueue(q)
	require.Eventually(t, func() bool { return r.has("next") }, 500*time.Millisecond, time.Millisecond)
	assert.False(t, r.has("closed"))
}

// heldQueue's head cannot be popped yet but still counts in its length (a
// delayed item, say): an empty pop with items left.
type heldQueue struct {
	*batchTestQueue
	hold atomic.Bool
}

func (q *heldQueue) DequeueBatchWithAckIds(items []any, ackIDs []string) (int, int, error) {
	if !q.hold.Load() {
		return q.batchTestQueue.DequeueBatchWithAckIds(items, ackIDs)
	}
	q.pops.Add(1)
	q.mu.Lock()
	defer q.mu.Unlock()
	return 0, len(q.items), nil
}

// shortOnceQueue's first pop takes one item whatever the count, as storage
// handing back an earlier failed call's items may.
type shortOnceQueue struct {
	*batchTestDistQueue
	done atomic.Bool
}

func (q *shortOnceQueue) DequeueBatchWithAckIds(items []any, ackIDs []string) (int, int, error) {
	if q.done.CompareAndSwap(false, true) {
		items, ackIDs = items[:1], ackIDs[:1]
	}
	return q.batchTestDistQueue.DequeueBatchWithAckIds(items, ackIDs)
}

// An empty pop with items left does not spin the event loop, Stop still
// returns, and the item is popped once it can be.
func TestBatchSourceEmptyPopWithItemsLeft(t *testing.T) {
	q := &heldQueue{batchTestQueue: &batchTestQueue{}}
	var r recorder
	wb := NewWorker(r.run)
	pq := wb.WithPersistentQueue(q)
	waitPops(t, q.batchTestQueue, 1)

	q.hold.Store(true)
	before := q.pops.Load()
	require.True(t, pq.Add("delayed"))
	time.Sleep(100 * time.Millisecond)
	assert.Less(t, q.pops.Load()-before, int64(5))

	q.hold.Store(false) // poppable now; no poke announces it
	require.Eventually(t, func() bool { return r.has("delayed") }, 1500*time.Millisecond, 10*time.Millisecond)

	q.hold.Store(true)
	require.True(t, pq.Add("held"))
	done := make(chan struct{})
	go func() { wb.StopAndWait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		q.hold.Store(false)
		t.Fatal("StopAndWait hangs")
	}
}

// A distributed adapter bound as a persistent queue is not subscribed, so it
// gets the persistent resync.
func TestBatchSourcePersistentBindingOfSubscribable(t *testing.T) {
	t.Parallel()
	q := &batchTestDistQueue{batchTestQueue: &batchTestQueue{}}
	var r recorder
	wb := NewWorker(r.run)
	defer wb.Stop()
	wb.WithPersistentQueue(q)
	waitPops(t, q.batchTestQueue, 1)
	q.push(jobBytes(t, "behind"))
	require.Eventually(t, func() bool { return r.has("behind") }, 1500*time.Millisecond, 10*time.Millisecond)
}

// The resync probes only while it is needed: never for a distributed source,
// not while another source shares the worker, not while the hint already
// reads ready, and not once the worker is paused or stopped. Many dry
// wake-ups keep one timer.
func TestBatchSourceResyncProbes(t *testing.T) {
	idle := func(t *testing.T, q *batchTestQueue, msg string) {
		t.Helper()
		before := q.lens.Load()
		time.Sleep(2300 * time.Millisecond)
		assert.Equal(t, before, q.lens.Load(), msg)
	}

	t.Run("distributed", func(t *testing.T) {
		t.Parallel()
		q := &batchTestDistQueue{batchTestQueue: &batchTestQueue{}}
		wb := NewWorker(func(Job[string]) {})
		defer wb.Stop()
		wb.WithDistributedQueue(q)
		waitPops(t, q.batchTestQueue, 1)
		idle(t, q.batchTestQueue, "wake-ups replace the resync")
	})

	t.Run("shared worker", func(t *testing.T) {
		t.Parallel()
		q := &batchTestQueue{}
		wb := NewWorker(func(Job[string]) {})
		defer wb.Stop()
		wb.WithPersistentQueue(q)
		waitPops(t, q, 1) // dry: a resync is pending
		wb.WithTopic(NewTopic[string]())
		before := q.lens.Load()
		time.Sleep(2300 * time.Millisecond)
		assert.LessOrEqual(t, q.lens.Load()-before, int64(0), "the strategy reads the exact length itself")
	})

	t.Run("ready", func(t *testing.T) {
		t.Parallel()
		q := &batchTestQueue{}
		release := make(chan struct{})
		wb := NewWorker(func(j Job[string]) {
			if j.Data() == "slow" {
				<-release
			}
		})
		defer wb.Stop()
		defer close(release)
		pq := wb.WithPersistentQueue(q)
		waitPops(t, q, 1)
		require.True(t, pq.Add("slow"))
		require.True(t, pq.Add("x")) // waits behind slow: the hint reads 1
		before := q.lens.Load()
		time.Sleep(1300 * time.Millisecond) // the pending resync fires
		assert.Equal(t, before, q.lens.Load(), "the hint already says ready")
	})

	t.Run("paused", func(t *testing.T) {
		t.Parallel()
		q := &batchTestQueue{}
		wb := NewWorker(func(Job[string]) {})
		defer wb.Stop()
		wb.WithPersistentQueue(q)
		waitPops(t, q, 1)
		require.NoError(t, wb.Pause())
		idle(t, q, "no probes while paused")
	})

	t.Run("stopped", func(t *testing.T) {
		t.Parallel()
		q := &batchTestQueue{}
		wb := NewWorker(func(Job[string]) {})
		wb.WithPersistentQueue(q)
		waitPops(t, q, 1)
		require.NoError(t, wb.StopAndWait())
		idle(t, q, "no probes after Stop")
	})

	t.Run("one timer", func(t *testing.T) {
		t.Parallel()
		q := &batchTestQueue{}
		wb := NewWorker(func(Job[string]) {})
		defer wb.Stop()
		w := internalWorker(wb)
		wb.WithPersistentQueue(q)
		waitPops(t, q, 1)
		for range 50 {
			w.notifyToPullNextJobs() // each wake-up reads the hint as 0
			time.Sleep(time.Millisecond)
		}
		before := q.lens.Load()
		time.Sleep(2100 * time.Millisecond)
		assert.LessOrEqual(t, q.lens.Load()-before, int64(3), "one probe a second")
	})
}

// An unparsable item reaches the error handler, and the next job still runs.
func TestBatchSourceBadItemReported(t *testing.T) {
	q := &batchTestQueue{}
	q.push([]byte("not json"))
	q.push(jobBytes(t, "ok"))
	var errs atomic.Int64
	var r recorder
	wb := NewWorker(r.run, WithErrHandler(func(_ Worker, err error) {
		if errors.Is(err, ErrParseJob) {
			errs.Add(1)
		}
	}))
	defer wb.Stop()
	wb.WithPersistentQueue(q)
	require.Eventually(t, func() bool { return r.has("ok") }, time.Second, time.Millisecond)
	assert.EqualValues(t, 1, errs.Load())
}

// slowEnqQueue runs hook inside Enqueue, before the item lands.
type slowEnqQueue struct {
	*batchTestQueue
	hook func()
}

func (q *slowEnqQueue) Enqueue(v any) bool {
	if q.hook != nil {
		q.hook()
	}
	return q.batchTestQueue.Enqueue(v)
}

// slowEnqPrioQueue is slowEnqQueue for priority storage.
type slowEnqPrioQueue struct{ *slowEnqQueue }

func (q *slowEnqPrioQueue) Enqueue(v any, _ int) bool { return q.slowEnqQueue.Enqueue(v) }

// The poke follows the storage's confirmation: a pop that runs while the add
// is in flight must not leave the item waiting for the resync.
func TestBatchSourcePokeAfterEnqueue(t *testing.T) {
	cases := []struct {
		name string
		bind func(wb IWorkerBinder[string], q *slowEnqQueue) func(string) bool
	}{
		{"persistent", func(wb IWorkerBinder[string], q *slowEnqQueue) func(string) bool {
			pq := wb.WithPersistentQueue(q)
			return func(s string) bool { return pq.Add(s) }
		}},
		{"persistent priority", func(wb IWorkerBinder[string], q *slowEnqQueue) func(string) bool {
			pq := wb.WithPersistentPriorityQueue(&slowEnqPrioQueue{q})
			return func(s string) bool { return pq.Add(s, 1) }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := &slowEnqQueue{batchTestQueue: &batchTestQueue{}}
			var r recorder
			wb := NewWorker(r.run)
			defer wb.Stop()
			w := internalWorker(wb)
			add := c.bind(wb, q)
			waitPops(t, q.batchTestQueue, 1)
			time.Sleep(1100 * time.Millisecond) // the bind's resync has run and re-armed: it is mid-period
			q.hook = func() {
				p := q.pops.Load()
				w.notifyToPullNextJobs() // another wake-up while the add is in flight
				for deadline := time.Now().Add(100 * time.Millisecond); q.pops.Load() == p && time.Now().Before(deadline); {
					time.Sleep(time.Millisecond)
				}
			}
			start := time.Now()
			require.True(t, add("x"))
			require.Eventually(t, func() bool { return r.has("x") }, 2*time.Second, time.Millisecond)
			assert.Less(t, time.Since(start), 400*time.Millisecond, "the add's own poke brings the pop")
		})
	}
}

// A dry hint read through the manager arms the resync: the classic loop's
// only contact with the source while parked jobs drain.
func TestCreditManagerLenArmsResync(t *testing.T) {
	q := &batchTestQueue{}
	// never active: the timer the arm starts finds the worker inactive and stops
	s := newBatchAckSource(q, q, newWorker(func(iJob[string]) {}, WithAutoRun(false)))
	s.dryAt.Store(s.pokes.Load()) // seen empty, nothing announced since
	require.False(t, s.resyncing.Load())
	m := &creditManager[iJob[string]]{directManager: directManager[iJob[string]]{src: s}, cs: s}
	assert.Equal(t, 0, m.Len())
	assert.True(t, s.resyncing.Load(), "a dry hint arms the resync")
}

// Removing a source the manager does not hold keeps the credit manager, so
// dispatch stays on credit.
func TestCreditManagerRemoveOtherKeepsIt(t *testing.T) {
	q := &batchTestQueue{}
	s := newBatchAckSource(q, q, newWorker(func(iJob[string]) {}, WithAutoRun(false)))
	m := singleManager[iJob[string]](s, 0)
	cm, ok := m.(*creditManager[iJob[string]])
	require.True(t, ok)
	other := newBatchAckSource(q, q, newWorker(func(iJob[string]) {}, WithAutoRun(false)))
	assert.Same(t, cm, cm.remove(other))
	assert.IsType(t, &emptyManager[iJob[string]]{}, cm.remove(s))
}

// Jobs a pause parked count in NumPending when the batch source is alone,
// and run after a Resume.
func TestBatchSourceNumPendingCountsParked(t *testing.T) {
	q := &batchTestQueue{}
	release := make(chan struct{})
	started := make(chan struct{}, 10)
	var ran atomic.Int64
	wb := NewWorker(func(Job[string]) { started <- struct{}{}; <-release; ran.Add(1) }, WithConcurrency(1), WithAutoRun(false))
	defer wb.Stop()
	w := internalWorker(wb)
	topic := NewTopic[string]()
	sub := wb.WithTopic(topic)
	for i := range 10 {
		topic.Publish(fmt.Sprint(i))
	}
	require.NoError(t, wb.Start()) // one batch of 10
	<-started                      // the first job runs: the whole batch was taken
	require.NoError(t, wb.Pause()) // the rest of the batch parks once the running job ends
	wb.WithPersistentQueue(q)
	sub.Unsubscribe()
	_, isCredit := w.manager().(*creditManager[iJob[string]])
	require.True(t, isCredit)
	release <- struct{}{}
	wb.WaitUntilPaused()
	parked := int(w.nParked.Load())
	require.Positive(t, parked)
	assert.Equal(t, parked, wb.NumPending())

	close(release)
	require.NoError(t, wb.Resume())
	require.Eventually(t, func() bool { return ran.Load() == 10 }, time.Second, time.Millisecond, "parked jobs run first")
}
