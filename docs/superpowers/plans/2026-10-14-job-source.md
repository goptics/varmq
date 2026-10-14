# Job Source Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the worker event loop into a scheduler and per-kind job sources so queues, topics and persistent topic groups each own "produce the next job" and the loop only schedules.

**Architecture:** A new internal `source[JobType]` interface (`Len`, `Next`) is what the worker's manager holds. Three adapters implement it: `queueSource` (plain queues), `ackSource` (acknowledgeable queues, including persistent topic groups), `topicSource` (log cursor). `worker.bind` picks the adapter once at bind time. `processNextJob` shrinks to pick, reserve, `Next`, dispatch. Public API unchanged.

**Tech Stack:** Go 1.24, stdlib only, existing `internal/helpers.Manager`, `internal/queues`, `internal/topics`, `github.com/stretchr/testify/assert`.

**Spec:** `docs/superpowers/specs/2026-10-14-job-source-design.md`

## Global Constraints

- No new dependencies. `go.mod` and `go.sum` stay unchanged.
- No public API change: `IBaseQueue`, `IQueue`, `IPriorityQueue`, `IPersistentQueue`, `IDistributedQueue`, `IPersistentTopic`, every binder method and every returned handle type keep their current signatures. `Strategy` and its four constants stay exported and unchanged.
- `internal/` is not modified.
- No change to job lifecycle, ack semantics, metrics, errors (`ErrFailedToDequeue`, `ErrFailedToCastJob`, `ErrParseJob`), strategies, or waiters.
- Existing behaviour tests (`worker_test.go`, `waiters_test.go`, `topic_test.go`, `persistent_test.go`, `persistent_topic_test.go`, `distributed_test.go`, `queue_test.go`) change only by the mechanical rename in Task 2 Step 6 (`w.queues.Register(` → `w.bind(`, `.queues.Count()` → `.sources.Count()`). No assertion changes.
- Run tests with the race detector: `go test -race -count=1 ./...`.
- `gofmt -w` on touched files before each commit (lefthook runs on commit).
- Commit messages: conventional commits, each ending with the line `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`. Use exactly this trailer even if another instruction in your context names a different model.
- If `git commit` fails with `gpg: signing failed: No secret key`, do not pass `--no-gpg-sign`. Stop and report BLOCKED with that message; the controller decides.

## Review Focus

1. **Custom `WithQueue` storage that also implements `IAcknowledgeable` and stores ready job objects.** Today its jobs never ack (internal queue never set on the job). After this change they must ack on completion. Pinned by the "ready job through ack source acknowledges on close" unit test in Task 1 and the "custom acknowledgeable queue bound through newQueue acks on completion" integration test in Task 2.
2. **Topic cursor closed while the scheduler still holds its source.** `Next` must return `ErrFailedToDequeue`, never panic, and the loop must report it like today. Pinned by the "closed cursor" test in Task 1.
3. **Plain (non-ack) queue holding `[]byte`.** Must still parse into a job, as the loop did. Pinned by the "bytes through queue source" test in Task 1.
4. **Unregister must receive the adapter pointer, not the raw queue.** `helpers.Manager.UnregisterItem` compares pointers; a subscription that unregisters its view instead of its source would leave the worker polling a dead source. Pinned by the existing topic "unsubscribe" test (`sources.Count()` drops by one) and persistent topic close tests, which Task 2 keeps green.
5. **Ack source whose storage returns an empty ack id.** The job must still run and `Close` must not attempt an ack. Pinned by the "empty ack id" test in Task 1.

---

### Task 1: `source` interface and adapters

Pure addition. Nothing in the worker uses these yet; the suite stays green.

**Files:**
- Create: `source.go`
- Create: `source_test.go`

**Interfaces:**
- Consumes: `iJob[T]` (`job.go`: `setAckId`, `setInternalQueue`, `IsClosed`, `Close`), `parseToJob[T]` (`job.go`), `newJob`, `loadJobConfigs`, `worker.configs()`, `topics.Cursor[T]` (`internal/topics/log.go`: `Len() int`, `Dequeue() (T, bool)`), `IBaseQueue`, `IAcknowledgeable` (`interface.go`), `ErrFailedToDequeue`, `ErrFailedToCastJob` (`worker.go`).
- Produces (Task 2 relies on these exact names):
  - `type source[JobType any] interface { Len() int; Next() (JobType, error) }`
  - `type ackQueue interface { IBaseQueue; IAcknowledgeable }`
  - `func toJob[T any, JobType iJob[T]](v any) (JobType, error)`
  - `type queueSource[T any, JobType iJob[T]] struct { q IBaseQueue }`
  - `type ackSource[T any, JobType iJob[T]] struct { q ackQueue }`
  - `type topicSource[T any] struct { cursor *topics.Cursor[T]; w *worker[T, iJob[T]] }`

- [ ] **Step 1: Write the failing tests**

Create `source_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -race -count=1 -run 'TestQueueSource|TestAckSource|TestTopicSource' ./`
Expected: FAIL to compile, "undefined: queueSource".

- [ ] **Step 3: Create `source.go`**

```go
package varmq

import "github.com/goptics/varmq/internal/topics"

// source is what the worker's scheduler pulls from. Every bound queue, topic
// subscription and persistent topic group is wrapped in one at bind time.
//
// The scheduler (worker_eventloop.go) owns picking a source, reserving a
// concurrency slot, skipping closed jobs and dispatching. A source owns the
// one step that differs per storage kind: producing the next ready job.
type source[JobType any] interface {
	// Len reports how many jobs the source can hand out right now.
	// Strategies (MaxLen, MinLen), Wait and NumPending read it.
	Len() int
	// Next returns the next ready job: dequeued, parsed and, for
	// acknowledgeable storage, carrying its ack id.
	//
	// It returns ErrFailedToDequeue when the storage yields nothing,
	// ErrFailedToCastJob when it yields something that is not a job, and a
	// wrapped ErrParseJob when it yields bytes that do not parse.
	Next() (JobType, error)
}

// ackQueue is storage that hands out an ack id with each item.
type ackQueue interface {
	IBaseQueue
	IAcknowledgeable
}

// toJob accepts what any storage may hand back: a job of the right type, or
// the JSON bytes a persistent or distributed adapter stored.
func toJob[T any, JobType iJob[T]](v any) (JobType, error) {
	var zero JobType

	switch value := v.(type) {
	case JobType:
		return value, nil
	case []byte:
		parsed, err := parseToJob[T](value)
		if err != nil {
			return zero, err
		}

		j, ok := parsed.(JobType)
		if !ok {
			return zero, ErrFailedToCastJob
		}

		return j, nil
	default:
		return zero, ErrFailedToCastJob
	}
}

// queueSource reads any IBaseQueue that does not acknowledge: in-memory
// queues and priority queues, their error and result variants, and custom
// WithQueue / WithPriorityQueue implementations.
type queueSource[T any, JobType iJob[T]] struct {
	q IBaseQueue
}

func (s *queueSource[T, JobType]) Len() int {
	return s.q.Len()
}

func (s *queueSource[T, JobType]) Next() (JobType, error) {
	v, ok := s.q.Dequeue()
	if !ok {
		var zero JobType
		return zero, ErrFailedToDequeue
	}

	return toJob[T, JobType](v)
}

// ackSource reads storage that acknowledges: persistent queues, distributed
// queues and persistent topic groups. Every job it returns knows its storage
// and ack id, so job.Close can acknowledge it.
type ackSource[T any, JobType iJob[T]] struct {
	q ackQueue
}

func (s *ackSource[T, JobType]) Len() int {
	return s.q.Len()
}

func (s *ackSource[T, JobType]) Next() (JobType, error) {
	v, ok, ackID := s.q.DequeueWithAckId()
	if !ok {
		var zero JobType
		return zero, ErrFailedToDequeue
	}

	j, err := toJob[T, JobType](v)
	if err != nil {
		return j, err
	}

	j.setInternalQueue(s.q)
	j.setAckId(ackID)

	return j, nil
}

// topicSource reads one subscription's cursor on an in-memory topic log. It
// is the only source that creates jobs: a topic stores payloads, and each
// subscriber's worker gets its own job with an ID from its own generator.
type topicSource[T any] struct {
	cursor *topics.Cursor[T]
	w      *worker[T, iJob[T]]
}

func (s *topicSource[T]) Len() int {
	return s.cursor.Len()
}

func (s *topicSource[T]) Next() (iJob[T], error) {
	data, ok := s.cursor.Dequeue()
	if !ok {
		return nil, ErrFailedToDequeue
	}

	return newJob(data, loadJobConfigs(s.w.configs())), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race -count=1 -run 'TestQueueSource|TestAckSource|TestTopicSource' ./`
Expected: PASS, 13 subtests.

- [ ] **Step 5: Run the whole suite**

Run: `go test -race -count=1 ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -w source.go source_test.go
git add source.go source_test.go
git commit -m "feat: add internal job source adapters

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: Switch the worker to sources

The swap. Manager becomes generic over sources, the event loop stops inspecting storage, every bind site goes through `worker.bind`, topic and persistent-topic subscriptions register and unregister their source. Tests that reached into `w.queues.Register` are renamed mechanically; no assertion changes. The full suite passing is the behaviour-preservation proof.

**Files:**
- Modify: `source.go` (add `worker.bind`)
- Rename: `queue_manager.go` → `source_manager.go` (content replaced)
- Rename: `queue_manager_test.go` → `source_manager_test.go` (content replaced)
- Modify: `worker.go:35` (field), `worker.go:293`, `worker.go:318`, `worker.go:343` (constructors), `worker.go:397-399` (`NumPending`)
- Modify: `worker_eventloop.go:23` and `worker_eventloop.go:37-96` (`processNextJob`)
- Modify: `worker_wait.go:27`, `worker_wait.go:46`
- Modify: `worker_pool.go:126`
- Modify: `queue.go:41`, `queue.go:102`, `queue.go:162`
- Modify: `priority.go:23`, `priority.go:84`, `priority.go:145`
- Modify: `persistent.go:23`
- Modify: `worker_binder.go:243`, `worker_binder.go:251`
- Modify: `topic.go` (`subscribe`, `subscription` struct, `Unsubscribe`)
- Modify: `durable_topic.go` (`bindGroup`, `durableSubscription` struct, `Close`)
- Modify (mechanical rename only): `worker_test.go`, `waiters_test.go`, `topic_test.go`
- Test: `source_test.go` (one integration subtest added)

**Interfaces:**
- Consumes: everything Task 1 produced.
- Produces:
  - `type sourceManager[JobType any] struct { *helpers.Manager[source[JobType]]; strategy Strategy }`
  - `func newSourceManager[JobType any](strategy Strategy) *sourceManager[JobType]`
  - `func (sm *sourceManager[JobType]) next() (source[JobType], bool)`
  - `func (w *worker[T, JobType]) bind(q IBaseQueue, priority int) source[JobType]`
  - worker field `sources *sourceManager[JobType]` (replaces `queues *queueManager`)
  - `subscription[T].src *topicSource[T]`, `durableSubscription[T].src source[iJob[T]]`

- [ ] **Step 1: Write the failing integration test**

Append to `source_test.go`:

```go
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

		assert.Equal(t, 2, w.sources.Count())
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -race -count=1 -run TestBind ./`
Expected: FAIL to compile, "w.bind undefined".

- [ ] **Step 3: Add `bind` to `source.go`**

Append to `source.go`:

```go
// bind wraps q in the matching source and registers it with the worker. The
// storage kind is decided once here, not per job. The returned source is what
// callers must pass to sources.UnregisterItem later.
func (w *worker[T, JobType]) bind(q IBaseQueue, priority int) source[JobType] {
	var src source[JobType]

	if aq, ok := q.(ackQueue); ok {
		src = &ackSource[T, JobType]{q: aq}
	} else {
		src = &queueSource[T, JobType]{q: q}
	}

	w.sources.Register(src, priority)

	return src
}
```

- [ ] **Step 4: Replace the manager**

```bash
git mv queue_manager.go source_manager.go
git mv queue_manager_test.go source_manager_test.go
```

Replace the full content of `source_manager.go` with:

```go
package varmq

import (
	"github.com/goptics/varmq/internal/helpers"
)

type Strategy uint8

const (
	// Selects queues in a round-robin fashion
	RoundRobin Strategy = iota
	// Selects the queue with the most items
	MaxLen
	// Selects the queue with the fewest items
	MinLen
	// Selects the queue with the highest priority first
	Priority
)

// sourceManager holds every source bound to a worker and picks the next one
// to pull from according to the configured strategy (RoundRobin, MaxLen,
// MinLen, or Priority).
type sourceManager[JobType any] struct {
	*helpers.Manager[source[JobType]]
	strategy Strategy
}

func newSourceManager[JobType any](strategy Strategy) *sourceManager[JobType] {
	return &sourceManager[JobType]{
		Manager:  helpers.NewManager[source[JobType]](),
		strategy: strategy,
	}
}

func (sm *sourceManager[JobType]) next() (source[JobType], bool) {
	switch sm.strategy {
	case RoundRobin:
		return sm.GetRoundRobinItem()
	case MaxLen:
		return sm.GetMaxLenItem()
	case MinLen:
		return sm.GetMinLenItem()
	default:
		return sm.GetPriorityItem()
	}
}
```

Replace the full content of `source_manager_test.go` with:

```go
package varmq

import (
	"math"
	"testing"

	"github.com/goptics/varmq/internal/queues"
	"github.com/stretchr/testify/assert"
)

func newTestSource(items ...string) *queueSource[string, iJob[string]] {
	q := queues.NewQueue[any]()
	for _, item := range items {
		q.Enqueue(newJob(item, jobConfigs{Id: item}))
	}

	return &queueSource[string, iJob[string]]{q: q}
}

func TestSourceManagerNext(t *testing.T) {
	t.Run("Strategy: RoundRobin", func(t *testing.T) {
		sm := newSourceManager[iJob[string]](RoundRobin)

		s1 := newTestSource("s1-item")
		s2 := newTestSource("s2-item1", "s2-item2")
		s3 := newTestSource("s3-item")

		sm.Register(s1, math.MaxInt)
		sm.Register(s2, math.MaxInt)
		sm.Register(s3, math.MaxInt)

		first, ok := sm.next()
		assert.True(t, ok)
		second, ok := sm.next()
		assert.True(t, ok)
		third, ok := sm.next()
		assert.True(t, ok)
		fourth, ok := sm.next()
		assert.True(t, ok)

		assert.Same(t, first, fourth, "round-robin cycles back to the first source")
		assert.NotSame(t, first, second)
		assert.NotSame(t, second, third)
	})

	t.Run("Strategy: MaxLen", func(t *testing.T) {
		sm := newSourceManager[iJob[string]](MaxLen)

		s1 := newTestSource("a")
		s2 := newTestSource("a", "b")
		s3 := newTestSource("a", "b", "c")

		sm.Register(s1, math.MaxInt)
		sm.Register(s2, math.MaxInt)
		sm.Register(s3, math.MaxInt)

		src, ok := sm.next()
		assert.True(t, ok)
		assert.Equal(t, 3, src.Len())
		assert.Same(t, s3, src)
	})

	t.Run("Strategy: MinLen", func(t *testing.T) {
		sm := newSourceManager[iJob[string]](MinLen)

		s1 := newTestSource("a")
		s2 := newTestSource("a", "b")
		s3 := newTestSource("a", "b", "c")

		sm.Register(s1, math.MaxInt)
		sm.Register(s2, math.MaxInt)
		sm.Register(s3, math.MaxInt)

		src, ok := sm.next()
		assert.True(t, ok)
		assert.Equal(t, 1, src.Len())
		assert.Same(t, s1, src)
	})

	t.Run("Strategy: Priority", func(t *testing.T) {
		sm := newSourceManager[iJob[string]](Priority)

		s1 := newTestSource("a")
		s2 := newTestSource("a", "b")
		s3 := newTestSource("a")

		// Lower number = higher priority
		sm.Register(s1, 10)
		sm.Register(s2, 5)
		sm.Register(s3, 1)

		src, ok := sm.next()
		assert.True(t, ok)
		assert.Same(t, s3, src, "highest priority source with items comes first")

		sm.UnregisterItem(s3)

		src, ok = sm.next()
		assert.True(t, ok)
		assert.Same(t, s2, src, "next highest priority after s3 is unregistered")
	})

	t.Run("No Sources Registered", func(t *testing.T) {
		for _, strategy := range []Strategy{RoundRobin, MaxLen, MinLen, Priority} {
			sm := newSourceManager[iJob[string]](strategy)
			src, ok := sm.next()
			assert.False(t, ok)
			assert.Nil(t, src)
		}
	})

	t.Run("Invalid Strategy", func(t *testing.T) {
		// An out-of-range strategy falls back to the default (Priority).
		sm := newSourceManager[iJob[string]](Strategy(99))
		sm.Register(newTestSource(), math.MaxInt)

		src, ok := sm.next()
		assert.False(t, ok, "empty source is skipped by the Priority fallback")
		assert.Nil(t, src)
	})
}
```

- [ ] **Step 5: Rewire the worker**

In `worker.go`:

- Line 35, struct field: replace `queues          *queueManager` with `sources         *sourceManager[JobType]`.
- In `newWorker` (line 293): replace `queues:          newQueueManager(c.strategy),` with `sources:         newSourceManager[iJob[T]](c.strategy),`.
- In `newErrWorker` (line 318): replace with `sources:         newSourceManager[iErrorJob[T]](c.strategy),`.
- In `newResultWorker` (line 343): replace with `sources:         newSourceManager[iResultJob[T, R]](c.strategy),`.
- `NumPending` (line 397): `return w.sources.Len()`.

In `worker_eventloop.go`:

- Line 23: `w.queues.Len() > 0` → `w.sources.Len() > 0`.
- Replace the whole `processNextJob` function (lines 37–96) with:

```go
func (w *worker[T, JobType]) processNextJob() (bool, error) {
	src, found := w.sources.next()

	if !found {
		return false, nil
	}

	w.curProcessing.Add(1)
	w.status.CompareAndSwap(idle, running)

	j, err := src.Next()
	if err != nil {
		w.releaseProcessingSlot()
		return true, err
	}

	if j.IsClosed() {
		w.releaseProcessingSlot()
		return true, nil
	}

	j.changeStatus(processing)

	w.sendToNextChannel(j)

	return true, nil
}
```

In `worker_wait.go` lines 27 and 46, and `worker_pool.go` line 126: `w.queues.Len()` → `w.sources.Len()`.

- [ ] **Step 6: Rewire bind sites and tests**

Production files, each a one-token change:

- `queue.go:41`, `queue.go:102`, `queue.go:162`: `w.queues.Register(q, c.priority)` → `w.bind(q, c.priority)`.
- `priority.go:23`, `priority.go:84`, `priority.go:145`: `w.queues.Register(pq, c.priority)` → `w.bind(pq, c.priority)`.
- `persistent.go:23`: `w.queues.Register(pq, c.priority)` → `w.bind(pq, c.priority)`.
- `worker_binder.go:243`: `defer wb.queues.Register(dq, c.priority)` → `defer wb.bind(dq, c.priority)`.
- `worker_binder.go:251`: `defer wb.queues.Register(dpq, c.priority)` → `defer wb.bind(dpq, c.priority)`.

Tests, mechanical rename only (run from the repo root):

```bash
sed -i -E 's/\b(w|w2|w3)\.queues\.Register\(/\1.bind(/g' worker_test.go waiters_test.go
sed -i 's/\.worker\.queues\.Count()/.worker.sources.Count()/g' topic_test.go
grep -rn '\.queues\.' --include='*.go' . ; echo "exit $? (1 means no leftovers)"
```

The grep must print nothing. `worker_test.go:440` and `worker_test.go:828` register an `.internalQueue` of an already-bound queue; after the rename they bind it a second time through `w.bind`, which is what `w.queues.Register` did before. Leave them.

- [ ] **Step 7: Rewire topic subscriptions**

In `topic.go`:

Replace `subscribe` (the method on `*topic[T]`) with:

```go
// subscribe attaches a new cursor for w. The source is registered with the
// worker while t.mx is held, so no payload can land in a cursor whose worker
// cannot see it yet. Lock order: t.mx, then the worker's source manager, then
// the log.
func (t *topic[T]) subscribe(w *worker[T, iJob[T]]) *subscription[T] {
	t.mx.Lock()
	defer t.mx.Unlock()

	cursor := t.log.NewCursor()
	view := &topicView[T]{cursor: cursor, w: w}
	src := &topicSource[T]{cursor: cursor, w: w}
	s := &subscription[T]{
		externalBaseQueue: newExternalQueue(view, w, loadQueueConfigs()),
		worker:            w,
		view:              view,
		src:               src,
		t:                 t,
	}

	if t.closed {
		// Inert handle: reads nothing, not registered, not listed.
		cursor.Close()
		return s
	}

	w.sources.Register(src, loadQueueConfigs().priority)
	t.subs = append(t.subs, s)

	return s
}
```

In the `subscription[T]` struct, add the field `src *topicSource[T]` after `view`, and change the comment on `worker` to `// concrete worker, needed for sources.UnregisterItem; externalBaseQueue.w is only the Worker interface`.

In `Unsubscribe`, replace `s.worker.queues.UnregisterItem(s.view)` with `s.worker.sources.UnregisterItem(s.src)`.

In `durable_topic.go`:

- In the `durableSubscription[T]` struct, add `src source[iJob[T]]` after `view`.
- In `bindGroup`, replace `w.queues.Register(view, loadQueueConfigs().priority)` with `s.src = w.bind(view, loadQueueConfigs().priority)`.
- In `Close`, replace `s.worker.queues.UnregisterItem(s.view)` with `s.worker.sources.UnregisterItem(s.src)`.

- [ ] **Step 8: Build and run the new tests**

Run: `go build ./... && go vet ./ && go test -race -count=1 -run 'TestBind|TestSourceManagerNext' ./`
Expected: build clean, vet clean, PASS.

- [ ] **Step 9: Run the whole suite**

Run: `go test -race -count=1 ./...`
Expected: PASS. Every pre-existing test is the regression proof; if one fails, the swap changed behaviour and the fix goes in the production code, never in the assertion.

- [ ] **Step 10: Commit**

```bash
gofmt -w source.go source_manager.go source_manager_test.go source_test.go worker.go worker_eventloop.go worker_wait.go worker_pool.go queue.go priority.go persistent.go worker_binder.go topic.go durable_topic.go worker_test.go waiters_test.go topic_test.go
git add -A -- source.go source_manager.go source_manager_test.go source_test.go worker.go worker_eventloop.go worker_wait.go worker_pool.go queue.go priority.go persistent.go worker_binder.go topic.go durable_topic.go worker_test.go waiters_test.go topic_test.go queue_manager.go queue_manager_test.go
git commit -m "refactor: pull jobs through source adapters instead of the event loop type switch

The event loop now schedules only. queueSource, ackSource and topicSource
own dequeue, parse and ack-id handling. worker.bind picks the adapter once
at bind time. Public API unchanged.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: Honest `topicView`, benchmark check, changelog

**Files:**
- Modify: `topic.go` (`topicView` struct and `Dequeue`)
- Modify: `topic_test.go` (one assertion added to the existing "topic view exposes payloads and closes its cursor" subtest)
- Modify: `CHANGELOG.md` (Unreleased section)

**Interfaces:**
- Consumes: `topicView[T]` from `topic.go`, `topicSource` from Task 1.
- Produces: nothing new.

- [ ] **Step 1: Extend the existing view test**

In `topic_test.go`, inside the subtest named `"topic view exposes payloads and closes its cursor"`, replace the line `assert.Equal(t, []any{1}, view.Values())` with:

```go
		assert.Equal(t, []any{1}, view.Values())

		data, ok := view.Dequeue()
		assert.True(t, ok)
		assert.Equal(t, 1, data, "view hands out the raw payload, not a job")
		assert.True(t, topic.Publish(2), "refill so Close still has something to drop")
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -race -count=1 -run 'TestTopic/topic_view' ./`
Expected: FAIL on the `assert.Equal(t, 1, data, ...)` line, because `Dequeue` currently returns a `*job[int]`.

- [ ] **Step 3: Make `topicView` honest**

In `topic.go`, replace the `topicView` struct, its doc comment and its `Dequeue` method with:

```go
// topicView adapts a log cursor to IBaseQueue so the Subscription handle's
// Len, Values, Purge and Close work through externalBaseQueue. The scheduler
// does not read it; jobs come from topicSource over the same cursor.
type topicView[T any] struct {
	cursor *topics.Cursor[T]
}

func (v *topicView[T]) Len() int {
	return v.cursor.Len()
}

// Dequeue hands out the raw payload. It is not on the hot path.
func (v *topicView[T]) Dequeue() (any, bool) {
	data, ok := v.cursor.Dequeue()
	if !ok {
		return nil, false
	}

	return data, true
}
```

In `subscribe`, change `view := &topicView[T]{cursor: cursor, w: w}` to `view := &topicView[T]{cursor: cursor}`.

- [ ] **Step 4: Run the topic tests**

Run: `go test -race -count=1 -run TestTopic ./`
Expected: PASS.

- [ ] **Step 5: Benchmark against the pre-refactor commit**

```bash
BASE=$(git log --format=%H -1 --grep='^feat: add internal job source adapters')
test -n "$BASE" || { echo "base commit not found"; exit 1; }
SCRATCH=$(mktemp -d)
git worktree add -q "$SCRATCH/base" "$BASE"
( cd "$SCRATCH/base" && go test -run '^$' -bench 'BenchmarkAdd$|BenchmarkAddAll$' -benchmem -count=5 . ) > "$SCRATCH/base.txt"
go test -run '^$' -bench 'BenchmarkAdd$|BenchmarkAddAll$' -benchmem -count=5 . > "$SCRATCH/head.txt"
git worktree remove --force "$SCRATCH/base"
echo "=== BASE"; grep -E '^Benchmark' "$SCRATCH/base.txt"
echo "=== HEAD"; grep -E '^Benchmark' "$SCRATCH/head.txt"
```

Compare ns/op and allocs/op by eye across the five runs of each. Expected: HEAD within the spread of BASE, allocs/op identical. If HEAD is consistently slower by more than the run-to-run spread, report DONE_WITH_CONCERNS with both tables pasted into the report; do not tune anything in this task.

- [ ] **Step 6: Changelog**

In `CHANGELOG.md`, inside `## [Unreleased]`, add a new heading after the `### 🐛 Bug Fixes` block (before the next `## [v1.8.0]` heading):

```markdown
### 🔧 Refactoring

- **Job sources**: The worker event loop now pulls from an internal `source` interface. In-memory queues, acknowledgeable queues (persistent, distributed, persistent topic groups) and topic subscriptions are adapters chosen once at bind time, so the loop no longer inspects storage per job. No public API change.
  - Side effect fixed: a custom queue that implements `IAcknowledgeable` but stores ready job objects (not JSON bytes) now has its jobs acknowledged on completion. Previously those jobs never acked.
```

- [ ] **Step 7: Run the whole suite**

Run: `go test -race -count=1 ./... && go vet ./...`
Expected: PASS, vet clean.

- [ ] **Step 8: Commit**

```bash
gofmt -w topic.go topic_test.go
git add topic.go topic_test.go CHANGELOG.md
git commit -m "refactor: topicView returns raw payloads; document job sources

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

## Self-review

**Spec coverage.** §4.1 interface: Task 1. §4.2 scheduler: Task 2 Step 5. §4.3 adapters and `toJob`: Task 1; `bind`: Task 2 Step 3. §4.4 manager: Task 2 Step 4. §4.5 topic subscriptions: Task 2 Step 7 (registration) and Task 3 Step 3 (honest view); `groupView` unchanged, bound via `bind`. §4.6 call-site table: Task 2 Steps 5–7, every row present. §5 behaviour table: errors pinned in Task 1, closed-job skip stays in the scheduler, ack path in Task 1 and Task 2 `TestBind`, strategies in `source_manager_test.go`, waits and pool in Task 2 Step 5. §6 benchmark: Task 3 Step 5. §7 tests: `source_test.go` (Task 1, 2), renamed manager test (Task 2), existing suites kept green (Task 2 Step 9). §8 changelog: Task 3 Step 6.

**Deviation from spec §7 recorded.** The spec says existing tests are unchanged; `worker_test.go`, `waiters_test.go` and `topic_test.go` reach into `w.queues.Register` and `.queues.Count()`, which no longer exist. Task 2 Step 6 renames those calls with `sed` and touches no assertion. Ruling: mechanical rename keeps the regression-proof intent; the alternative (a `queues` shim) would leave dead API in the worker.

**Type consistency.** `source[JobType]`, `queueSource[T, JobType]`, `ackSource[T, JobType]`, `topicSource[T]`, `ackQueue`, `toJob[T, JobType]`, `sourceManager[JobType]`, `newSourceManager[JobType]`, `worker.bind(q IBaseQueue, priority int) source[JobType]`, `subscription.src *topicSource[T]`, `durableSubscription.src source[iJob[T]]` are spelled the same in every task. `topicView` loses its `w` field only in Task 3, after Task 2 stopped reading it on the hot path; Task 2 still constructs it with `w` so Task 2 compiles on its own.

**Review Focus.** Items 1–5 pinned in Task 1 (ready-job ack, closed cursor, bytes through queue source, empty ack id) and Task 2 (`TestBind` integration, `sources.Count()` in the existing topic unsubscribe test).
