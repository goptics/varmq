package varmq

import (
	"math"
	"sync/atomic"
	"time"
	"weak"

	"github.com/goptics/varmq/internal/pool"
	"github.com/goptics/varmq/internal/topics"
)

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
	// purgeGen is the counter Purge bumps. The scheduler stamps each batch
	// with it, and the worker closes, instead of running, a job whose batch
	// is older than the counter. nil for sources that never batch.
	purgeGen() *atomic.Uint64
}

// queuePaused reports whether pz is a paused queue. It is the scheduler's gate:
// a hint read before asking storage, which is the real gate.
func queuePaused(pz pool.Holder) bool {
	return pz != nil && pz.Paused()
}

// holderOf is q's pause, nil (not a typed nil) if q cannot pause.
func holderOf(q any) pool.Holder {
	if pz, ok := q.(QueuePauser); ok {
		return pz
	}
	return nil
}

// pauseRefusals is QueuePauser's optional PauseRefusals (see QueuePauser).
type pauseRefusals interface{ PauseRefusals() uint64 }

// pauseGate is a source's view of its queue's pause.
type pauseGate struct {
	pz pool.Holder   // q's pause (QueuePauser), nil if it has none
	ep pauseRefusals // nil unless pz has one
}

func gateOf(q any) pauseGate {
	g := pauseGate{pz: holderOf(q)}
	g.ep, _ = q.(pauseRefusals)
	return g
}

// epoch is the refusal count read before a dequeue, for emptyErr.
func (g pauseGate) epoch() uint64 {
	if g.ep == nil {
		return 0
	}
	return g.ep.PauseRefusals()
}

// emptyErr is why a dequeue that began at refusal count e came back empty:
// a pause, now or counted since (a Pause and Resume both between Len and
// the check), is "nothing to do"; anything else is a failure the user
// hears about.
func (g pauseGate) emptyErr(e uint64) error {
	if queuePaused(g.pz) || g.ep != nil && g.ep.PauseRefusals() != e {
		return errNoReadySource
	}
	return ErrFailedToDequeue
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
	batch int         // decided once in bind: 1 for priority storage
	purge *purgeState // shared by every worker bound to q when q supports it; nil if it never batches
	pauseGate
}

// Len is 0 while the queue is paused: nothing to hand out.
func (s *queueSource[T, JobType]) Len() int {
	if queuePaused(s.pz) {
		return 0
	}
	return s.q.Len()
}

// holder is the pause the scheduler stamps each batch with, so the worker
// parks the unstarted rest of a batch whose queue got paused.
func (s *queueSource[T, JobType]) holder() pool.Holder { return s.pz }

func (s *queueSource[T, JobType]) maxBatch() int {
	return s.batch
}

// purgeGen is nil for priority storage: a batch of one starts at once, so
// nothing waits for Purge to cancel.
func (s *queueSource[T, JobType]) purgeGen() *atomic.Uint64 {
	if s.batch == 1 || s.purge == nil {
		return nil
	}

	return &s.purge.gen
}

// purgeStateOf is what the Queue handle's Purge drives.
func (s *queueSource[T, JobType]) purgeStateOf() *purgeState {
	if s.batch == 1 {
		return nil
	}

	return s.purge
}

func (s *queueSource[T, JobType]) Next() (JobType, error) {
	e := s.epoch()
	v, ok := s.q.Dequeue()
	if !ok {
		var zero JobType
		return zero, s.emptyErr(e)
	}

	return toJob[T, JobType](v)
}

// ackSource reads acknowledging storage: persistent and distributed queues.
// Each job it returns carries its storage and ack id so job.Close can ack it.
type ackSource[T any, JobType iJob[T]] struct {
	q ackQueue
	pauseGate
}

// Len is 0 while the queue is paused: nothing to hand out.
func (s *ackSource[T, JobType]) Len() int {
	if queuePaused(s.pz) {
		return 0
	}
	return s.q.Len()
}

// maxBatch is 1: a dequeued message stays unacknowledged until its job is
// done, so a batch would hide messages from other consumers and delay acks.
func (s *ackSource[T, JobType]) maxBatch() int {
	return 1
}

func (s *ackSource[T, JobType]) purgeGen() *atomic.Uint64 {
	return nil
}

func (s *ackSource[T, JobType]) Next() (JobType, error) {
	e := s.epoch()
	v, ok, ackID := s.q.DequeueWithAckId()
	if !ok {
		var zero JobType
		return zero, s.emptyErr(e)
	}

	j, err := toJob[T, JobType](v)
	if err != nil {
		var zero JobType
		return zero, err
	}

	if j.IsClosed() {
		return j, nil // scheduler skips it; never mutate a job the user already closed
	}

	j.setAck(s.q, ackID)

	return j, nil
}

// creditSource is a source that pops many jobs per storage call and keeps a
// ready hint, so the scheduler needs no length probe between pops. The
// worker reads it through creditManager while it is the only source.
type creditSource[JobType any] interface {
	source[JobType]
	// ready is the hint: a positive count when a pop may find a job, 0 when
	// the storage was last seen empty and nothing has announced work since.
	// It does no I/O. Len stays the exact storage length.
	ready() int
	// fetch pops up to want jobs in one storage call and hands each to emit,
	// in pop order. Event loop only.
	fetch(want int, emit func(JobType), report func(error)) error
	// armResync starts the persistent resync, if the source needs one and
	// none is pending.
	armResync()
	// reset forgets that the storage was seen empty.
	reset()
}

// neverDry is batchAckSource.dryAt while the storage is not known to be
// empty: no pokes count reaches it.
const neverDry = math.MaxUint64

// resyncEvery is how often a dry persistent source rechecks its storage for
// items no poke announced.
const resyncEvery = time.Second

// batchAckSource reads IBatchAcknowledgeable storage. Each pop says how many
// items are left, and each add or wake-up pokes the source, so the scheduler
// knows whether to pop again without asking the storage.
//
// No wake-up is lost: fetch reads pokes before the storage call, so a poke
// that lands while the call is in flight keeps ready above 0 even if the
// call comes back empty, and every poke is followed by a signal to the
// event loop.
type batchAckSource[T any, JobType iJob[T]] struct {
	ackSource[T, JobType] // Len (exact), Next (one item, for strategyManager), maxBatch 1, purgeGen nil

	bq         IBatchAcknowledgeable
	w          *worker[T, JobType]
	subscribed atomic.Bool   // bound as a distributed queue: wake-ups poke; else every add pokes in this process
	remaining  atomic.Int64  // storage length after the last pop
	pokes      atomic.Uint64 // +1 whenever work may have appeared
	dryAt      atomic.Uint64 // pokes when the storage was last seen empty; neverDry if it was not
	resyncing  atomic.Bool   // a resync timer is pending
	items      []any         // fetch's buffers
	ackIDs     []string
}

func newBatchAckSource[T any, JobType iJob[T]](q ackQueue, bq IBatchAcknowledgeable, w *worker[T, JobType]) *batchAckSource[T, JobType] {
	s := &batchAckSource[T, JobType]{ackSource: ackSource[T, JobType]{q: q, pauseGate: gateOf(q)}, bq: bq, w: w}
	s.dryAt.Store(neverDry)

	return s
}

// ready is 0 while the queue is paused; the Resume pokes.
func (s *batchAckSource[T, JobType]) ready() int {
	if queuePaused(s.pz) {
		return 0
	}
	if r := s.remaining.Load(); r > 0 {
		return int(r)
	}

	if s.pokes.Load() != s.dryAt.Load() {
		return 1
	}

	return 0
}

// poke records that work may have appeared: an add confirmed by the
// storage, or a distributed wake-up. Callers signal the event loop after it.
func (s *batchAckSource[T, JobType]) poke() {
	s.pokes.Add(1)
}

func (s *batchAckSource[T, JobType]) reset() {
	s.dryAt.Store(neverDry)
}

// fetch hands a job that does not parse to report and leaves it pending, and
// skips one the user already closed, as ackSource.Next does. The returned
// error is the storage call's.
func (s *batchAckSource[T, JobType]) fetch(want int, emit func(JobType), report func(error)) error {
	if cap(s.items) < want {
		s.items, s.ackIDs = make([]any, want), make([]string, want)
	}
	items, ackIDs := s.items[:want], s.ackIDs[:want]

	g := s.pokes.Load()
	n, remaining, err := s.bq.DequeueBatchWithAckIds(items, ackIDs)
	if err != nil {
		s.remaining.Store(0)
		s.markDry(g)
		return err
	}

	if n == 0 {
		remaining = 0 // nothing to hand out now, whatever the count says: wait for a poke
	}
	s.remaining.Store(int64(remaining))
	// A short pop ran the storage dry. So did a full one that emptied a
	// persistent queue: every add after it pokes. A distributed consumer
	// whose full pop emptied the queue probes once more, since it did not
	// join the idle set and an enqueue would not wake it.
	if n < want || (!s.subscribed.Load() && remaining == 0) {
		s.markDry(g)
	} else {
		s.dryAt.Store(neverDry)
	}

	for i := range n {
		j, err := toJob[T, JobType](items[i])
		if err != nil {
			report(err)
			continue
		}

		if !j.IsClosed() { // never mutate a job the user already closed
			j.setAck(s.q, ackIDs[i])
			emit(j)
		}
	}
	clear(items[:n])

	return nil
}

// markDry records that the storage was empty as of pokes value g.
func (s *batchAckSource[T, JobType]) markDry(g uint64) {
	s.dryAt.Store(g)
	s.armResync()
}

// armResync covers what no poke announces to a persistent source: items a
// visibility timeout requeued, and items another process or handle added.
func (s *batchAckSource[T, JobType]) armResync() {
	if s.subscribed.Load() || s.resyncing.Load() || !s.resyncing.CompareAndSwap(false, true) {
		return
	}

	time.AfterFunc(resyncEvery, s.resync)
}

// resync checks the exact length once per resyncEvery while the worker is
// active, the hint is dry and this is the worker's only source, and pokes
// when items wait. Otherwise it stops; the next time the hint reads 0 arms it
// again. It clears resyncing first, so an arm racing with the stop either
// sees it cleared or is followed by the checks below.
func (s *batchAckSource[T, JobType]) resync() {
	s.resyncing.Store(false)
	if !s.w.IsActive() || s.ready() > 0 || !s.sole() {
		return
	}

	if s.Len() > 0 {
		s.poke()
		s.w.notifyToPullNextJobs()
		return
	}

	s.armResync()
}

// sole reports whether s is the worker's only source. With others bound the
// hint is not kept, and the strategy reads the exact length on every wake.
func (s *batchAckSource[T, JobType]) sole() bool {
	cm, ok := s.w.manager().(*creditManager[JobType])
	return ok && cm.cs == creditSource[JobType](s)
}

// pokeOf returns src's poke if src keeps a ready hint, else nil.
func pokeOf[JobType any](src source[JobType]) func() {
	if p, ok := src.(interface{ poke() }); ok {
		return p.poke
	}

	return nil
}

// topicSource reads one subscription's cursor on an in-memory topic log. It
// is the only source that creates jobs: a topic stores payloads, and each
// subscriber's worker gets its own job with an ID from its own generator.
type topicSource[T any] struct {
	cursor *topics.Cursor[T]
	w      *worker[T, iJob[T]]
	purge  *purgeState // this subscription's own: a cursor is never shared
}

func (s *topicSource[T]) Len() int {
	return s.cursor.Len()
}

func (s *topicSource[T]) maxBatch() int {
	return maxBatch
}

func (s *topicSource[T]) purgeGen() *atomic.Uint64 {
	return &s.purge.gen
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

	_ creditSource[iJob[int]] = (*batchAckSource[int, iJob[int]])(nil)
)

// bind wraps q in the matching source and registers it with the worker. The
// storage kind is decided once here, not per job. The returned source is what
// callers must pass to removeSource later.
func (w *worker[T, JobType]) bind(q IBaseQueue, priority int) source[JobType] {
	var src source[JobType]
	g := gateOf(q)

	if aq, ok := q.(ackQueue); ok {
		if bq, ok := q.(IBatchAcknowledgeable); ok {
			src = newBatchAckSource(aq, bq, w)
		} else {
			src = &ackSource[T, JobType]{q: aq, pauseGate: g}
		}
	} else if _, ok := q.(IPriorityQueue); ok {
		src = &queueSource[T, JobType]{q: q, batch: 1, pauseGate: g}
	} else {
		purge := purgeStateFor(q)
		purge.register(w.parkHook())
		src = &queueSource[T, JobType]{q: q, batch: maxBatch, purge: purge, pauseGate: g}
	}

	w.onResume(q, src)
	w.addSource(src, priority)
	registerQueue(q)
	if qi, ok := q.(QueueInspector); ok {
		if id := qi.QueueID(); id != "" {
			w.queueIDs.Store(id, struct{}{})
		}
	}

	if _, ok := src.(creditSource[JobType]); ok {
		w.notifyToPullNextJobs() // the storage may already hold items; no add will announce them
	}

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

// pauseHook is what a queue's Resume runs on a worker bound to it. The
// worker holds it (pauseHooks); the storage holds it only weakly, so binding
// a queue never keeps a discarded worker alive.
type pauseHook struct {
	paused  func()
	resumed func()
	cancel  func()
}

// onResume wakes the worker when q, a PauseNotifier, is resumed by any
// handle or process: src's ready hint is poked, then the event loop. Every
// source kind registers, so every bound worker wakes.
func (w *worker[T, JobType]) onResume(q IBaseQueue, src source[JobType]) {
	pn, ok := q.(PauseNotifier)
	if !ok {
		return
	}

	poke := pokeOf(src)
	h := &pauseHook{paused: func() { w.parkHeld.Store(true) }, resumed: func() {
		if poke != nil {
			poke()
		}
		w.notifyToPullNextJobs()
	}}
	wh := weak.Make(h)
	var self atomic.Pointer[func()]
	cancel := pn.OnPauseChange(func(paused bool) {
		h := wh.Value()
		if h == nil { // worker collected: drop the hook from the storage
			if c := self.Load(); c != nil {
				(*c)()
			}
			return
		}
		if paused {
			h.paused()
		} else {
			h.resumed()
		}
	})
	self.Store(&cancel)
	h.cancel = cancel
	w.pauseHooks.Store(src, h)
}

// removeSource unbinds src, swapping in the manager for the remaining sources.
func (w *worker[T, JobType]) removeSource(src source[JobType]) {
	if h, ok := w.pauseHooks.LoadAndDelete(src); ok {
		h.(*pauseHook).cancel()
	}

	w.sourcesMx.Lock()
	defer w.sourcesMx.Unlock()

	m := w.manager().remove(src)
	w.sources.Store(managerBox[JobType]{m})

	if _, ok := m.(*creditManager[JobType]); ok {
		w.notifyToPullNextJobs() // its hint was reset: one pop finds out, after the swap so the loop sees the new manager
	}
}
