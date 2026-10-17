package varmq

import "github.com/goptics/varmq/internal/topics"

// source is what the worker's scheduler pulls from. Every bound queue and topic
// subscription is wrapped in one at bind time.
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
	// maxBatch is the most jobs the scheduler may hand one worker from this
	// source per wake-up. In-memory FIFO storage batches; storage that
	// acknowledges returns 1 so one worker never prefetches messages other
	// consumers could take, and priority storage returns 1 so a job that
	// arrives after a batch was taken is not stuck behind lower priorities.
	maxBatch() int
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
	q     IBaseQueue
	batch int // decided once in bind: 1 for priority storage
}

func (s *queueSource[T, JobType]) Len() int {
	return s.q.Len()
}

func (s *queueSource[T, JobType]) maxBatch() int {
	return s.batch
}

func (s *queueSource[T, JobType]) Next() (JobType, error) {
	v, ok := s.q.Dequeue()
	if !ok {
		var zero JobType
		return zero, ErrFailedToDequeue
	}

	return toJob[T, JobType](v)
}

// ackSource reads storage that acknowledges: persistent and distributed
// queues. Every job it returns knows its storage
// and ack id, so job.Close can acknowledge it.
type ackSource[T any, JobType iJob[T]] struct {
	q ackQueue
}

func (s *ackSource[T, JobType]) Len() int {
	return s.q.Len()
}

// maxBatch is 1: every dequeued message is unacknowledged until its job is
// done, so holding a batch would hide messages from other consumers and
// stretch the time before they are acknowledged.
func (s *ackSource[T, JobType]) maxBatch() int {
	return 1
}

func (s *ackSource[T, JobType]) Next() (JobType, error) {
	v, ok, ackID := s.q.DequeueWithAckId()
	if !ok {
		var zero JobType
		return zero, ErrFailedToDequeue
	}

	j, err := toJob[T, JobType](v)
	if err != nil {
		var zero JobType
		return zero, err
	}

	if j.IsClosed() {
		return j, nil // scheduler skips it; never mutate a job the user already closed
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

func (s *topicSource[T]) maxBatch() int {
	return maxBatch
}

func (s *topicSource[T]) Next() (iJob[T], error) {
	data, ok := s.cursor.Dequeue()
	if !ok {
		return nil, ErrFailedToDequeue
	}

	return newJob(data, loadJobConfigs(s.w.configs())), nil
}

var (
	_ source[iJob[int]] = (*queueSource[int, iJob[int]])(nil)
	_ source[iJob[int]] = (*ackSource[int, iJob[int]])(nil)
	_ source[iJob[int]] = (*topicSource[int])(nil)
)

// bind wraps q in the matching source and registers it with the worker. The
// storage kind is decided once here, not per job. The returned source is what
// callers must pass to removeSource later.
func (w *worker[T, JobType]) bind(q IBaseQueue, priority int) source[JobType] {
	var src source[JobType]

	if aq, ok := q.(ackQueue); ok {
		src = &ackSource[T, JobType]{q: aq}
	} else if _, ok := q.(IPriorityQueue); ok {
		src = &queueSource[T, JobType]{q: q, batch: 1}
	} else {
		src = &queueSource[T, JobType]{q: q, batch: maxBatch}
	}

	w.addSource(src, priority)

	return src
}

// managerBox gives atomic.Value one concrete type to hold, whichever
// sourceManager implementation is current.
type managerBox[JobType any] struct {
	sourceManager[JobType]
}

// manager returns the worker's current sourceManager.
func (w *worker[T, JobType]) manager() sourceManager[JobType] {
	return w.sources.Load().(managerBox[JobType]).sourceManager
}

// addSource binds src, swapping in the manager for the new set of sources.
func (w *worker[T, JobType]) addSource(src source[JobType], priority int) {
	w.sourcesMx.Lock()
	defer w.sourcesMx.Unlock()

	w.sources.Store(managerBox[JobType]{w.manager().add(src, priority, w.Configs.strategy)})
}

// removeSource unbinds src, swapping in the manager for the remaining sources.
func (w *worker[T, JobType]) removeSource(src source[JobType]) {
	w.sourcesMx.Lock()
	defer w.sourcesMx.Unlock()

	w.sources.Store(managerBox[JobType]{w.manager().remove(src)})
}
