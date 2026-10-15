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
// callers must pass to sources.UnregisterItem later.
func (w *worker[T, JobType]) bind(q IBaseQueue, priority int) source[JobType] {
	var src source[JobType]

	if aq, ok := q.(ackQueue); ok {
		src = &ackSource[T, JobType]{q: aq}
	} else {
		src = &queueSource[T, JobType]{q: q}
	}

	w.attach(src, priority)

	return src
}

// attach makes src pullable by the scheduler. The first source is stored as
// is; the second promotes both into a sourceManager that applies the worker's
// strategy; later ones register into that manager.
func (w *worker[T, JobType]) attach(src source[JobType], priority int) {
	w.srcMx.Lock()
	defer w.srcMx.Unlock()

	cur := w.src.Load()

	switch {
	case cur == nil:
		w.srcPriority = priority
		w.src.Store(&src)
	default:
		if m, ok := (*cur).(*sourceManager[JobType]); ok {
			m.Register(src, priority)
			return
		}

		m := newSourceManager[JobType](w.Configs.strategy)
		m.Register(*cur, w.srcPriority)
		m.Register(src, priority)

		var promoted source[JobType] = m
		w.src.Store(&promoted)
	}
}

// detach stops the scheduler from pulling src. A manager stays a manager
// even when it drops to one source; collapsing back is not worth the code.
func (w *worker[T, JobType]) detach(src source[JobType]) {
	w.srcMx.Lock()
	defer w.srcMx.Unlock()

	cur := w.src.Load()
	if cur == nil {
		return
	}

	if m, ok := (*cur).(*sourceManager[JobType]); ok {
		m.UnregisterItem(src)
		return
	}

	if *cur == src {
		w.src.Store(nil)
	}
}

// currentSource returns what the scheduler pulls from, if anything is bound.
func (w *worker[T, JobType]) currentSource() (source[JobType], bool) {
	cur := w.src.Load()
	if cur == nil {
		return nil, false
	}

	return *cur, true
}

// numSources counts bound sources: 0, 1, or the manager's count.
func (w *worker[T, JobType]) numSources() int {
	cur, ok := w.currentSource()
	if !ok {
		return 0
	}

	if m, ok := cur.(*sourceManager[JobType]); ok {
		return m.Count()
	}

	return 1
}
