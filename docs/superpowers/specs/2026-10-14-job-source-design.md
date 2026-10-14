# Job source design: one scheduler, many sources

Date: 2026-10-14
Branch: `feat/topic-fanout` (on top of the topic and persistent topic work)
Status: draft, awaiting review

## 1. Problem

The worker's event loop does two jobs at once.

Scheduling: pick a bound source by strategy, reserve a concurrency slot, skip closed jobs, hand the job to a pool goroutine, release the slot, wake waiters. This is the same for every kind of source.

Fetching: get the next item out of the source and turn it into a job. This differs per kind: an in-memory queue yields a job, a persistent queue yields `[]byte` plus an ack id, a topic yields a raw payload that must be wrapped in a fresh job.

Today fetching leaks into the scheduler and the sources lie about their role:

- `processNextJob` ([worker_eventloop.go:37](../../../worker_eventloop.go)) type-switches on `IAcknowledgeable`, then on `JobType` vs `[]byte`, then parses, then sets ack id and internal queue. That is fetch logic inside the scheduler.
- `topicView.Dequeue` ([topic.go:215](../../../topic.go)) creates a job inside a method named `Dequeue`. A queue that manufactures its items is not a queue.
- Everything registered with the worker is typed `IBaseQueue` and managed by `queueManager`, even when it is a log cursor or a persistent topic group.

Adding a fifth source kind today means another branch in the loop.

## 2. Goal

Split the two responsibilities at the seam between them. The scheduler owns steps that are the same for every source. Each source kind owns, in its own adapter, the one step that differs: produce the next ready job.

Public API unchanged. Behaviour unchanged. Internal refactor only.

## 3. Non-goals

- No public API change. `IBaseQueue`, `IQueue`, `IPriorityQueue`, `IPersistentQueue`, `IDistributedQueue`, `IPersistentTopic`, all binder methods and all returned handle types stay as they are.
- No change to job lifecycle, ack semantics, metrics, errors, strategies, or waiters.
- No new source kinds. Topic and persistent topic are already in the tree; this change re-homes them.
- No performance work beyond proving no regression.

## 4. Design

### 4.1 The seam

```go
// source is what the scheduler pulls from. Every bound queue, topic
// subscription and persistent topic group is wrapped in one.
type source[JobType any] interface {
	// Len reports how many jobs the source can hand out right now.
	// Strategies (MaxLen, MinLen), Wait and NumPending read it.
	Len() int
	// Next returns the next ready job: dequeued, parsed, ack id attached.
	// It returns an error when the underlying storage yields nothing
	// (ErrFailedToDequeue), yields something that is not a job
	// (ErrFailedToCastJob), or yields bytes that do not parse (ErrParseJob).
	Next() (JobType, error)
}
```

`helpers.Manager` is already generic over anything with `Len()`, so the manager becomes `Manager[source[JobType]]` with no change to `internal/helpers`.

### 4.2 Scheduler after the change

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

The type switch, `parseToJob`, `setAckId` and `setInternalQueue` leave this function. Everything else in `worker_eventloop.go` is untouched.

### 4.3 Adapters (new file `source.go`)

Three adapters cover every bind site. One shared helper turns a raw value into a job.

```go
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
```

**queueSource**: any `IBaseQueue` that is not acknowledgeable (in-memory queue and priority queue, error and result variants, custom `WithQueue` / `WithPriorityQueue` implementations).

```go
type queueSource[T any, JobType iJob[T]] struct{ q IBaseQueue }

func (s *queueSource[T, JobType]) Len() int { return s.q.Len() }

func (s *queueSource[T, JobType]) Next() (JobType, error) {
	v, ok := s.q.Dequeue()
	if !ok {
		var zero JobType
		return zero, ErrFailedToDequeue
	}
	return toJob[T, JobType](v)
}
```

**ackSource**: any `IBaseQueue` that also implements `IAcknowledgeable` (persistent queue, distributed queue, persistent topic group via `groupView`).

```go
type ackSource[T any, JobType iJob[T]] struct {
	q interface {
		IBaseQueue
		IAcknowledgeable
	}
}

func (s *ackSource[T, JobType]) Len() int { return s.q.Len() }

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
```

Today `setInternalQueue` runs only on the `[]byte` branch and `setAckId` runs for every job. After the change both run for every job that came through an ack source, and neither runs for a queue source. Net effect is identical: a non-ack source always had an empty ack id and `ack()` short-circuits on it ([job.go:251](../../../job.go)); an ack source that stored a ready job instead of bytes previously never got its queue set and so could never ack, which was a latent bug, now fixed as a side effect. Note it in the changelog.

**topicSource**: wraps a topic cursor and the subscribing worker. This is the only adapter that creates jobs, and it says so.

```go
type topicSource[T any] struct {
	cursor *topics.Cursor[T]
	w      *worker[T, iJob[T]]
}

func (s *topicSource[T]) Len() int { return s.cursor.Len() }

func (s *topicSource[T]) Next() (iJob[T], error) {
	data, ok := s.cursor.Dequeue()
	if !ok {
		return nil, ErrFailedToDequeue
	}
	return newJob(data, loadJobConfigs(s.w.configs())), nil
}
```

**Picking the adapter** happens at bind time, in one place, so every bind site stays a one-liner:

```go
// bind wraps q in the right source and registers it. It returns the source
// so callers that unregister later (topic subscriptions) can hand it back.
func (w *worker[T, JobType]) bind(q IBaseQueue, priority int) source[JobType] {
	var src source[JobType]
	if aq, ok := q.(interface {
		IBaseQueue
		IAcknowledgeable
	}); ok {
		src = &ackSource[T, JobType]{q: aq}
	} else {
		src = &queueSource[T, JobType]{q: q}
	}
	w.sources.Register(src, priority)
	return src
}
```

The runtime check moves from per-job (today's type switch) to per-bind. A custom `IQueue` that also implements `IAcknowledgeable` keeps working exactly as it does now.

### 4.4 Manager

`queue_manager.go` becomes `source_manager.go`. Same strategies, same `next()`, generic over the job type:

```go
type sourceManager[JobType any] struct {
	*helpers.Manager[source[JobType]]
	strategy Strategy
}
```

`Strategy` and its constants stay exported and unchanged. The worker field `queues *queueManager` becomes `sources *sourceManager[JobType]`.

### 4.5 Topic subscriptions

`topicView` keeps implementing `IBaseQueue` because the `Subscription` handle's `Len`, `Purge` and `Close` go through `externalBaseQueue`, which wants one. Its `Dequeue` changes to return the raw payload, honest to the interface, and is no longer on the hot path. The worker registers a `topicSource` built from the same cursor; the subscription stores that source and unregisters it, not the view.

`groupView` in `durable_topic.go` is already an `IBaseQueue` + `IAcknowledgeable` adapter over `IPersistentTopic`. It is bound through `w.bind`, lands in an `ackSource`, and needs no change.

### 4.6 Call sites

| File | Today | After |
|---|---|---|
| `queue.go` (3 constructors) | `w.queues.Register(q, c.priority)` | `w.bind(q, c.priority)` |
| `priority.go` (3 constructors) | same | same |
| `persistent.go` | same | same |
| `worker_binder.go` (distributed ×2) | `defer wb.queues.Register(dq, c.priority)` | `defer wb.bind(dq, c.priority)` |
| `topic.go` `subscribe` | `w.queues.Register(view, ...)` | `src := &topicSource[T]{cursor, w}; w.sources.Register(src, ...)`; store `src` on the subscription |
| `topic.go` `Unsubscribe` | `UnregisterItem(s.view)` | `UnregisterItem(s.src)` |
| `durable_topic.go` `bindGroup` | `w.queues.Register(view, ...)` | `s.src = w.bind(view, ...)` |
| `durable_topic.go` `Unsubscribe` | `UnregisterItem(s.view)` | `UnregisterItem(s.src)` |
| `worker.go` ×3 constructors, `NumPending` | `queues: newQueueManager(...)`, `w.queues.Len()` | `sources: newSourceManager[JobType](...)`, `w.sources.Len()` |
| `worker_wait.go`, `worker_pool.go`, `worker_eventloop.go` | `w.queues.Len()` | `w.sources.Len()` |

`helpers.Manager.UnregisterItem` compares by pointer, so every unregister must receive the same adapter pointer that was registered. The two subscription types store it for that reason.

## 5. Behaviour preservation

| Behaviour | Where it lives after |
|---|---|
| Empty-after-Len race reports `ErrFailedToDequeue` | `queueSource.Next` / `ackSource.Next` |
| Non-job value reports `ErrFailedToCastJob` | `toJob` |
| Bad JSON reports `ErrParseJob` wrapped | `toJob` via `parseToJob` |
| Closed jobs skipped without running | scheduler |
| Ack on finish for persistent sources | `ackSource.Next` sets queue and ack id, `job.Close` acks as before |
| Strategy selection, round-robin index reset on unregister | `sourceManager`, unchanged `helpers.Manager` |
| `NumPending`, `Wait`, `WaitUntilIdle`, idle-worker trimming | read `sources.Len()` |
| Topic job gets subscriber's ID generator | `topicSource.Next` |
| Persistent topic at-least-once via group ack | `groupView` + `ackSource` |

## 6. Performance

One interface call (`Next`) replaces an inline type switch per job. Adapters are two-word structs, no allocation per job beyond what exists today. Run `make bench` on the base commit and on the result; `BenchmarkAdd` and `BenchmarkAddAll` must stay within noise. If they do not, the fallback is to make `queueSource` the concrete type the manager stores for the common case, but that is not expected to be needed.

## 7. Tests

- `source_test.go` (new): table test per adapter. `queueSource` with a job, with `[]byte`, with a string (cast error), with empty queue. `ackSource` with bytes: job gets ack id and internal queue; `Close` on the job calls `Acknowledge`. `topicSource`: job carries the payload and an ID from the worker generator; empty cursor errors.
- `queue_manager_test.go` → `source_manager_test.go`: same cases, registering sources instead of raw queues.
- `worker_test.go`, `waiters_test.go`, `topic_test.go`, `persistent_test.go`, `distributed_test.go`: unchanged on purpose. They are the behaviour-preservation proof. All pass under `go test -race -count=1 ./...`.
- `mocks/`: unchanged; they implement public interfaces.

## 8. Docs

- `CHANGELOG.md`, Unreleased, Refactoring: "Event loop now pulls from an internal `source` interface; queues, topics and persistent topic groups are adapters. No public API change." Plus one line on the ack-source fix in §4.3.
- No README change. Internals only.

## 9. Files

| File | Change |
|---|---|
| `source.go` | new: `source`, `toJob`, `queueSource`, `ackSource`, `topicSource`, `worker.bind` |
| `source_test.go` | new |
| `source_manager.go` | renamed from `queue_manager.go`, generic |
| `source_manager_test.go` | renamed from `queue_manager_test.go` |
| `worker_eventloop.go` | `processNextJob` shrinks to §4.2 |
| `worker.go` | field and constructor rename, `NumPending` |
| `worker_wait.go`, `worker_pool.go` | `queues` → `sources` |
| `queue.go`, `priority.go`, `persistent.go`, `worker_binder.go` | `Register` → `bind` |
| `topic.go` | `topicView.Dequeue` returns payload; `subscribe` registers `topicSource`; subscription stores and unregisters it |
| `durable_topic.go` | `bindGroup` uses `bind`; subscription stores and unregisters the source |
| `CHANGELOG.md` | refactor entry |

## 10. Open decisions

None. Naming (`source`, `bind`, `sourceManager`) is the proposal; rename is cheap at implementation time if a better word turns up.
