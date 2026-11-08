package varmq

// Durable handles: what WithQueue and WithPriorityQueue return for
// acknowledgeable storage (redisq, sqliteq, custom), and what NewQueue and
// NewPriorityQueue return for producers. Jobs go out as JSON; Add returns a
// remoteJob, so Wait, Err and Result work across processes when the storage
// reports outcomes (OutcomeWatcher), and report ErrDetached when it doesn't.
//
// H and G are the job and group handle types of the worker kind
// (EnqueuedJob/EnqueuedGroupJob, the Err and Result ones): a remoteJob[R]
// and remoteGroup[R] are all of them, so one type serves every kind.

// durableBase is the storage side shared by both handles.
type durableBase[T, R any] struct {
	*externalBaseQueue
	s    IBaseQueue // IPersistentQueue or IPersistentPriorityQueue
	poke func()     // a batch source's ready hint; nil otherwise
	cfg  func() configs
}

// bindDurable binds w to s: woken by notifications when s is ISubscribable,
// by the source's resync otherwise.
func bindDurable[T, R any, J iJob[T]](w *worker[T, J], s IBaseQueue, configs []QueueConfigFunc) durableBase[T, R] {
	c := loadQueueConfigs(configs...)
	src := w.bind(s, c.priority)
	if sq, ok := s.(ISubscribable); ok {
		subscribeQueue(w, sq, src)
	}

	return durableBase[T, R]{
		externalBaseQueue: newExternalQueue(s, w, c, nil), // never batches: nothing waits in a buffer
		s:                 s,
		poke:              pokeOf(src),
		cfg:               w.configs,
	}
}

// producerBase is durableBase with no worker: Add only.
func producerBase[T, R any](s IBaseQueue, configs []QueueConfigFunc) durableBase[T, R] {
	return durableBase[T, R]{
		externalBaseQueue: newExternalQueue(s, nil, loadQueueConfigs(configs...), nil),
		s:                 s,
		cfg:               newConfig,
	}
}

// add enqueues data through push and returns its watched handle. A job with
// no ID gets one when the storage reports outcomes; only a given ID dedupes.
func (d durableBase[T, R]) add(data T, configs []JobConfigFunc, push func(val []byte, onceID string) bool) (*remoteJob[R], bool) {
	c := loadJobConfigs(d.cfg(), configs...)
	if _, ok := d.s.(OutcomeWatcher); ok && c.Id == "" {
		c.Id = newRemoteID()
	}

	j := newJob(data, c)
	val, err := j.Json()
	if err != nil {
		return nil, false
	}

	rj := watchRemote[R](d.s, j.id) // before the push: never miss a fast outcome
	if !push(val, c.onceId(j.id)) {
		rj.Close()
		return nil, false
	}

	if d.poke != nil {
		d.poke() // after the storage confirmed: a pop must not mark it empty before the item lands
	}
	if d.w != nil {
		d.w.notifyToPullNextJobs()
	}

	return rj, true
}

func (d durableBase[T, R]) addAll(items []Item[T], push func(it Item[T], val []byte, onceID string) bool) *remoteGroup[R] {
	g := newRemoteGroup[R](len(items))
	for _, it := range items {
		cfg := []JobConfigFunc{}
		if it.ID != "" {
			cfg = append(cfg, WithJobId(generateGroupId(it.ID)))
		}
		if j, ok := d.add(it.Data, cfg, func(val []byte, id string) bool { return push(it, val, id) }); ok {
			g.track(j)
		}
	}
	return g
}

// durableQueue is the FIFO handle.
type durableQueue[T, R, H, G any] struct {
	durableBase[T, R]
}

func (q *durableQueue[T, R, H, G]) push(val []byte, id string) bool {
	return enqueue(q.s.(IQueue), val, id)
}

func (q *durableQueue[T, R, H, G]) Add(data T, configs ...JobConfigFunc) (H, bool) {
	j, ok := q.add(data, configs, q.push)
	if !ok {
		var zero H
		return zero, false
	}
	return any(j).(H), true
}

func (q *durableQueue[T, R, H, G]) AddAll(items []Item[T]) G {
	g := q.addAll(items, func(_ Item[T], val []byte, id string) bool { return q.push(val, id) })
	return any(g).(G)
}

func (q *durableQueue[T, R, H, G]) WorkflowItem(data T) (WorkflowItem, error) {
	return durableWorkflowItem(q.s, false, data)
}

// durablePriorityQueue is the priority handle.
type durablePriorityQueue[T, R, H, G any] struct {
	durableBase[T, R]
}

func (q *durablePriorityQueue[T, R, H, G]) push(val []byte, prio int, id string) bool {
	return enqueuePriority(q.s.(IPriorityQueue), val, prio, id)
}

func (q *durablePriorityQueue[T, R, H, G]) Add(data T, priority int, configs ...JobConfigFunc) (H, bool) {
	j, ok := q.add(data, configs, func(val []byte, id string) bool { return q.push(val, priority, id) })
	if !ok {
		var zero H
		return zero, false
	}
	return any(j).(H), true
}

func (q *durablePriorityQueue[T, R, H, G]) AddAll(items []Item[T]) G {
	g := q.addAll(items, func(it Item[T], val []byte, id string) bool { return q.push(val, it.Priority, id) })
	return any(g).(G)
}

func (q *durablePriorityQueue[T, R, H, G]) WorkflowItem(data T) (WorkflowItem, error) {
	return durableWorkflowItem(q.s, true, data)
}

// ProducerQueue is a FIFO handle on shared storage with no worker: a
// producer process. NewQueue makes it.
type ProducerQueue[T any] interface {
	TaskQueue[T] // a workflow task can target it
	IExternalBaseQueue
	// Add adds a job. The handle waits on its outcome, from whichever
	// process runs it.
	Add(data T, configs ...JobConfigFunc) (RemoteJob, bool)
}

// ProducerPriorityQueue is ProducerQueue for priority storage.
type ProducerPriorityQueue[T any] interface {
	TaskQueue[T]
	IExternalBaseQueue
	Add(data T, priority int, configs ...JobConfigFunc) (RemoteJob, bool)
}

// NewQueue is a producer handle on shared FIFO storage (rq.NewQueue("x"),
// sqliteq, custom): it adds jobs that workers in any process bound with
// WithQueue run. Use DecodeResult on a result worker's outcome.
func NewQueue[T any](s IPersistentQueue, configs ...QueueConfigFunc) ProducerQueue[T] {
	return &durableQueue[T, struct{}, RemoteJob, EnqueuedErrGroupJob]{producerBase[T, struct{}](s, configs)}
}

// NewPriorityQueue is NewQueue for priority storage.
func NewPriorityQueue[T any](s IPersistentPriorityQueue, configs ...QueueConfigFunc) ProducerPriorityQueue[T] {
	return &durablePriorityQueue[T, struct{}, RemoteJob, EnqueuedErrGroupJob]{producerBase[T, struct{}](s, configs)}
}
