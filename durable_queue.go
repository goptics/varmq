package varmq

import "sync"

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
	live *sync.Map // *remoteJob -> struct{}: handles not ended yet, for Purge
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
		live:              &sync.Map{},
	}
}

// producerBase is durableBase with no worker: Add only.
func producerBase[T, R any](s IBaseQueue, configs []QueueConfigFunc) durableBase[T, R] {
	return durableBase[T, R]{
		externalBaseQueue: newExternalQueue(s, nil, loadQueueConfigs(configs...), nil),
		s:                 s,
		cfg:               newConfig,
		live:              &sync.Map{},
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

	// A generated ID is new: watch before the push, so a fast outcome is
	// never missed. A given ID may name an earlier run whose record the
	// push clears (spec 6.13): watch after it, or the catch-up read could
	// report that run.
	var rj *remoteJob[R]
	watch := func() {
		rj = watchRemote(d.s, j.id, func(e *remoteJob[R]) { d.live.Delete(e) })
		d.live.Store(rj, struct{}{})
		if rj.IsClosed() { // ended before the Store: its Delete already ran
			d.live.Delete(rj)
		}
	}
	if !c.given {
		watch()
	}
	if !push(val, c.onceId(j.id)) {
		if rj != nil {
			rj.Close()
		}
		return nil, false
	}
	if c.given {
		watch()
	}

	if d.poke != nil {
		d.poke() // after the storage confirmed: a pop must not mark it empty before the item lands
	}
	if d.w != nil {
		d.w.notifyToPullNextJobs()
	}

	return rj, true
}

// Purge drops the waiting jobs and detaches this handle's live job
// handles (ErrDetached): a purged job never ends on its storage, so its Wait
// would block for good. OpenJob still sees jobs that do end.
// ponytail: storage does not say which jobs it dropped, so running jobs'
// handles detach too; per-ID purge if that matters.
func (d durableBase[T, R]) Purge() {
	d.externalBaseQueue.Purge()
	d.live.Range(func(k, _ any) bool {
		_ = k.(*remoteJob[R]).Close()
		return true
	})
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
	g.seal()
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

// RemoteResultJob is a RemoteJob whose worker is a ResultWorker: Result
// decodes its R.
type RemoteResultJob[R any] interface {
	RemoteJob
	Result() (R, error)
}

// ProducerResultQueue is ProducerQueue for jobs a ResultWorker runs.
type ProducerResultQueue[T, R any] interface {
	TaskQueue[T]
	IExternalBaseQueue
	Add(data T, configs ...JobConfigFunc) (RemoteResultJob[R], bool)
}

// ProducerResultPriorityQueue is ProducerResultQueue for priority storage.
type ProducerResultPriorityQueue[T, R any] interface {
	TaskQueue[T]
	IExternalBaseQueue
	Add(data T, priority int, configs ...JobConfigFunc) (RemoteResultJob[R], bool)
}

// NewResultQueue is NewQueue for jobs a ResultWorker[T, R] runs: Add's job
// has Result, as a bound ResultQueue's does.
func NewResultQueue[T, R any](s IPersistentQueue, configs ...QueueConfigFunc) ProducerResultQueue[T, R] {
	return &durableQueue[T, R, RemoteResultJob[R], EnqueuedResultGroupJob[R]]{producerBase[T, R](s, configs)}
}

// NewResultPriorityQueue is NewResultQueue for priority storage.
func NewResultPriorityQueue[T, R any](s IPersistentPriorityQueue, configs ...QueueConfigFunc) ProducerResultPriorityQueue[T, R] {
	return &durablePriorityQueue[T, R, RemoteResultJob[R], EnqueuedResultGroupJob[R]]{producerBase[T, R](s, configs)}
}

// NewQueue is a producer handle on shared FIFO storage (rq.NewQueue("x"),
// sqliteq, custom): it adds jobs that workers in any process bound with
// WithQueue run. Its job has Wait and Err; for a ResultWorker's R use
// NewResultQueue.
func NewQueue[T any](s IPersistentQueue, configs ...QueueConfigFunc) ProducerQueue[T] {
	return &durableQueue[T, struct{}, RemoteJob, EnqueuedErrGroupJob]{producerBase[T, struct{}](s, configs)}
}

// NewPriorityQueue is NewQueue for priority storage.
func NewPriorityQueue[T any](s IPersistentPriorityQueue, configs ...QueueConfigFunc) ProducerPriorityQueue[T] {
	return &durablePriorityQueue[T, struct{}, RemoteJob, EnqueuedErrGroupJob]{producerBase[T, struct{}](s, configs)}
}
