package varmq

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/goptics/varmq/internal/queues"
	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dedupLog records what reached a storage's push methods.
type dedupLog struct {
	mu     sync.Mutex
	seen   map[string]bool
	onceID []string // ids EnqueueOnce got, in order
	items  []any    // items EnqueueOnce got, in order
	prios  []int    // priorities EnqueueOnce got, in order (priority storage)
	plain  int      // Enqueue calls
	stored int      // items stored
}

// once reports whether id is new, recording the call.
func (l *dedupLog) once(item any, id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onceID = append(l.onceID, id)
	l.items = append(l.items, item)
	if l.seen == nil {
		l.seen = map[string]bool{}
	}
	if l.seen[id] {
		return false
	}
	l.seen[id] = true
	l.stored++
	return true
}

func (l *dedupLog) push() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.plain++
	l.stored++
}

type dedupDistQueue struct {
	*mocks.MockDistributedQueue
	log dedupLog
}

func (q *dedupDistQueue) Enqueue(item any) bool {
	q.log.push()
	return q.MockDistributedQueue.Enqueue(item)
}

func (q *dedupDistQueue) EnqueueOnce(item any, id string) bool {
	if q.log.once(item, id) {
		return q.MockDistributedQueue.Enqueue(item)
	}
	return true
}

type dedupDistPrioQueue struct {
	*mocks.MockDistributedPriorityQueue
	log dedupLog
}

func (q *dedupDistPrioQueue) Enqueue(item any, priority int) bool {
	q.log.push()
	return q.MockDistributedPriorityQueue.Enqueue(item, priority)
}

func (q *dedupDistPrioQueue) EnqueueOnce(item any, priority int, id string) bool {
	q.log.mu.Lock()
	q.log.prios = append(q.log.prios, priority)
	q.log.mu.Unlock()
	if q.log.once(item, id) {
		return q.MockDistributedPriorityQueue.Enqueue(item, priority)
	}
	return true
}

var (
	_ IDedupQueue         = (*dedupDistQueue)(nil)
	_ IDedupPriorityQueue = (*dedupDistPrioQueue)(nil)
	_ IDedupQueue         = (*queues.Queue[iJob[int]])(nil)
	_ IDedupPriorityQueue = (*queues.PriorityQueue[iJob[int]])(nil)
)

// A job with an id reaches storage that can dedupe through EnqueueOnce, so
// adding it twice stores it once and both Adds succeed; a job without one
// takes the plain push.
func TestAddWithIdUsesEnqueueOnce(t *testing.T) {
	noop := func(Job[string]) {}
	cases := []struct {
		name string
		add  func(t *testing.T) (add func(id string) bool, log *dedupLog)
	}{
		{"persistent", func(t *testing.T) (func(string) bool, *dedupLog) {
			s := &dedupDistQueue{MockDistributedQueue: mocks.NewMockDistributedQueue()}
			q := NewWorker(noop).WithPersistentQueue(s)
			t.Cleanup(func() { q.Worker().Stop() })
			return func(id string) bool { return q.Add("x", WithJobId(id)) }, &s.log
		}},
		{"persistent priority", func(t *testing.T) (func(string) bool, *dedupLog) {
			s := &dedupDistPrioQueue{MockDistributedPriorityQueue: mocks.NewMockDistributedPriorityQueue()}
			q := NewWorker(noop).WithPersistentPriorityQueue(s)
			t.Cleanup(func() { q.Worker().Stop() })
			return func(id string) bool { return q.Add("x", 3, WithJobId(id)) }, &s.log
		}},
		{"distributed", func(t *testing.T) (func(string) bool, *dedupLog) {
			s := &dedupDistQueue{MockDistributedQueue: mocks.NewMockDistributedQueue()}
			q := NewDistributedQueue[string](s)
			return func(id string) bool { return q.Add("x", WithJobId(id)) }, &s.log
		}},
		{"distributed priority", func(t *testing.T) (func(string) bool, *dedupLog) {
			s := &dedupDistPrioQueue{MockDistributedPriorityQueue: mocks.NewMockDistributedPriorityQueue()}
			q := NewDistributedPriorityQueue[string](s)
			return func(id string) bool { return q.Add("x", 3, WithJobId(id)) }, &s.log
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			add, log := c.add(t)

			assert.True(t, add("a"))
			assert.True(t, add("a"), "a repeat is accepted")
			assert.True(t, add("b"))
			assert.True(t, add(""), "no id")

			log.mu.Lock()
			defer log.mu.Unlock()
			assert.Equal(t, []string{"a", "a", "b"}, log.onceID)
			for i, item := range log.items {
				var j struct {
					Id   string `json:"id"`
					Data string `json:"data"`
				}
				assert.NoError(t, json.Unmarshal(item.([]byte), &j))
				assert.Equal(t, log.onceID[i], j.Id, "the job carries its id")
				assert.Equal(t, "x", j.Data)
			}
			if log.prios != nil {
				assert.Equal(t, []int{3, 3, 3}, log.prios, "the priority Add got")
			}
			assert.Equal(t, 1, log.plain, "a job without an id takes the plain push")
			assert.Equal(t, 3, log.stored, "a, b and the job without an id")
		})
	}
}

// memDedup is one in-memory queue kind, through what the dedupe tests use.
type memDedup struct {
	add    func(id string) (EnqueuedJob, bool)
	addAll func(ids ...string) EnqueuedGroupJob
	q      IExternalBaseQueue
	w      Worker
	ran    func() []string // ids of the jobs that ran, in order
}

// memDedupKinds builds each in-memory queue kind on a worker that is not
// started, so added jobs stay queued until the test calls Start.
func memDedupKinds(t *testing.T, cfg ...any) map[string]func() memDedup {
	cfg = append(cfg, WithAutoRun(false))

	newLog := func() (func(string), func() []string) {
		var mu sync.Mutex
		var ids []string
		return func(id string) {
				mu.Lock()
				ids = append(ids, id)
				mu.Unlock()
			}, func() []string {
				mu.Lock()
				defer mu.Unlock()
				return append([]string(nil), ids...)
			}
	}
	items := func(ids []string) []Item[int] {
		out := make([]Item[int], len(ids))
		for i, id := range ids {
			out[i] = Item[int]{ID: id, Data: i}
		}
		return out
	}
	stop := func(w Worker) { t.Cleanup(func() { _ = w.Stop() }) }

	return map[string]func() memDedup{
		"queue": func() memDedup {
			rec, ran := newLog()
			q := NewWorker(func(j Job[int]) { rec(j.ID()) }, cfg...).BindQueue()
			stop(q.Worker())
			return memDedup{
				add:    func(id string) (EnqueuedJob, bool) { return q.Add(0, WithJobId(id)) },
				addAll: func(ids ...string) EnqueuedGroupJob { return q.AddAll(items(ids)) },
				q:      q, w: q.Worker(), ran: ran,
			}
		},
		"priority": func() memDedup {
			rec, ran := newLog()
			q := NewWorker(func(j Job[int]) { rec(j.ID()) }, cfg...).BindPriorityQueue()
			stop(q.Worker())
			return memDedup{
				add:    func(id string) (EnqueuedJob, bool) { return q.Add(0, 1, WithJobId(id)) },
				addAll: func(ids ...string) EnqueuedGroupJob { return q.AddAll(items(ids)) },
				q:      q, w: q.Worker(), ran: ran,
			}
		},
		"err queue": func() memDedup {
			rec, ran := newLog()
			q := NewErrWorker(func(j Job[int]) error { rec(j.ID()); return nil }, cfg...).BindQueue()
			stop(q.Worker())
			return memDedup{
				add:    func(id string) (EnqueuedJob, bool) { return q.Add(0, WithJobId(id)) },
				addAll: func(ids ...string) EnqueuedGroupJob { return q.AddAll(items(ids)) },
				q:      q, w: q.Worker(), ran: ran,
			}
		},
		"err priority": func() memDedup {
			rec, ran := newLog()
			q := NewErrWorker(func(j Job[int]) error { rec(j.ID()); return nil }, cfg...).BindPriorityQueue()
			stop(q.Worker())
			return memDedup{
				add:    func(id string) (EnqueuedJob, bool) { return q.Add(0, 1, WithJobId(id)) },
				addAll: func(ids ...string) EnqueuedGroupJob { return q.AddAll(items(ids)) },
				q:      q, w: q.Worker(), ran: ran,
			}
		},
		"result queue": func() memDedup {
			rec, ran := newLog()
			q := NewResultWorker(func(j Job[int]) (int, error) { rec(j.ID()); return 0, nil }, cfg...).BindQueue()
			stop(q.Worker())
			return memDedup{
				add:    func(id string) (EnqueuedJob, bool) { return q.Add(0, WithJobId(id)) },
				addAll: func(ids ...string) EnqueuedGroupJob { return q.AddAll(items(ids)) },
				q:      q, w: q.Worker(), ran: ran,
			}
		},
		"result priority": func() memDedup {
			rec, ran := newLog()
			q := NewResultWorker(func(j Job[int]) (int, error) { rec(j.ID()); return 0, nil }, cfg...).BindPriorityQueue()
			stop(q.Worker())
			return memDedup{
				add:    func(id string) (EnqueuedJob, bool) { return q.Add(0, 1, WithJobId(id)) },
				addAll: func(ids ...string) EnqueuedGroupJob { return q.AddAll(items(ids)) },
				q:      q, w: q.Worker(), ran: ran,
			}
		},
	}
}

// An in-memory queue stores a job with an id once while it is queued: a
// repeat Add returns no job and false. The id is free again once a worker
// takes the job or Purge drops it.
func TestInMemoryAddWithId(t *testing.T) {
	for name, newKind := range memDedupKinds(t) {
		t.Run(name, func(t *testing.T) {
			t.Run("a repeat is refused while the job is queued", func(t *testing.T) {
				k := newKind()
				a, ok := k.add("a")
				require.True(t, ok)
				dup, ok := k.add("a")
				assert.False(t, ok)
				assert.Nil(t, dup)
				_, ok = k.add("b")
				assert.True(t, ok)
				_, ok = k.add("")
				assert.True(t, ok, "no id")
				_, ok = k.add("")
				assert.True(t, ok, "no id")
				assert.Equal(t, 4, k.q.Len())

				assert.NoError(t, k.w.Start())
				a.Wait()
				k.w.Wait()
				assert.ElementsMatch(t, []string{"a", "b", "", ""}, k.ran())
			})

			t.Run("the id is free once a worker took the job", func(t *testing.T) {
				k := newKind()
				assert.NoError(t, k.w.Start())
				a, ok := k.add("a")
				require.True(t, ok)
				a.Wait()
				again, ok := k.add("a")
				require.True(t, ok)
				again.Wait()
				assert.Equal(t, []string{"a", "a"}, k.ran())
			})

			t.Run("purge frees the ids", func(t *testing.T) {
				k := newKind()
				k.add("a")
				k.q.Purge()
				_, ok := k.add("a")
				assert.True(t, ok)
				assert.Equal(t, 1, k.q.Len())
			})

			t.Run("AddAll", func(t *testing.T) {
				k := newKind()
				g := k.addAll("", "", "x", "x")
				assert.Equal(t, 3, g.NumPending(), "items without an id all stored, the repeat x skipped")

				_, ok := k.add("x")
				assert.True(t, ok, "item ids are prefixed, so Add's x is another id")
				dup := k.addAll("x")
				require.Equal(t, 0, dup.NumPending())
				dup.Wait()

				assert.NoError(t, k.w.Start())
				g.Wait()
				k.w.Wait()

				k.addAll("x").Wait()
				assert.ElementsMatch(t, []string{"", "", "g:x", "x", "g:x"}, k.ran(),
					"the item's id is free once it ran, so x runs again")
			})

			t.Run("concurrent Adds of one id store one job", func(t *testing.T) {
				k := newKind()
				var stored atomic.Int32
				var wg sync.WaitGroup
				for range 32 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if _, ok := k.add("a"); ok {
							stored.Add(1)
						}
					}()
				}
				wg.Wait()
				assert.Equal(t, int32(1), stored.Load())
				assert.Equal(t, 1, k.q.Len())
			})
		})
	}

	// A generator never repeats an id for the same work, so in-memory queues
	// skip the check: even a constant generator stores every job.
	t.Run("a generated id is not checked", func(t *testing.T) {
		for name, newKind := range memDedupKinds(t, WithJobIdGenerator(func() string { return "same" })) {
			k := newKind()
			_, ok := k.add("")
			assert.True(t, ok, name)
			_, ok = k.add("")
			assert.True(t, ok, name)
			assert.Equal(t, 2, k.q.Len(), name)
		}
	})
}

// The mocks embed the in-memory queues but store JSON bytes: they must not
// pick up the in-memory EnqueueOnce, which would hold ids it can never free
// and skip the mocks' own Enqueue.
func TestMocksDoNotDeduplicate(t *testing.T) {
	noop := func(Job[string]) {}
	cases := []struct {
		name string
		add  func(t *testing.T) (add func(id string) bool, fail func(), notified *int)
	}{
		{"persistent", func(t *testing.T) (func(string) bool, func(), *int) {
			s := mocks.NewMockPersistentQueue()
			q := NewWorker(noop, WithAutoRun(false)).WithPersistentQueue(s)
			return func(id string) bool { return q.Add("x", WithJobId(id)) }, func() { s.ShouldFailEnqueue = true }, new(int)
		}},
		{"persistent priority", func(t *testing.T) (func(string) bool, func(), *int) {
			s := mocks.NewMockPersistentPriorityQueue()
			q := NewWorker(noop, WithAutoRun(false)).WithPersistentPriorityQueue(s)
			return func(id string) bool { return q.Add("x", 1, WithJobId(id)) }, func() { s.ShouldFailEnqueue = true }, new(int)
		}},
		{"distributed", func(t *testing.T) (func(string) bool, func(), *int) {
			s := mocks.NewMockDistributedQueue()
			n := new(int)
			s.Subscribe(func(string) { *n++ })
			q := NewDistributedQueue[string](s)
			return func(id string) bool { return q.Add("x", WithJobId(id)) }, func() { s.ShouldFailEnqueue = true }, n
		}},
		{"distributed priority", func(t *testing.T) (func(string) bool, func(), *int) {
			s := mocks.NewMockDistributedPriorityQueue()
			n := new(int)
			s.Subscribe(func(string) { *n++ })
			q := NewDistributedPriorityQueue[string](s)
			return func(id string) bool { return q.Add("x", 1, WithJobId(id)) }, func() { s.ShouldFailEnqueue = true }, n
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			add, fail, notified := c.add(t)
			assert.True(t, add("a"))
			assert.True(t, add("a"), "stored again: the mock does not deduplicate")
			if strings.HasPrefix(c.name, "distributed") {
				assert.Equal(t, 2, *notified, "subscribers hear of every Add")
			}
			fail()
			assert.False(t, add("b"), "ShouldFailEnqueue holds for a job with an id")
		})
	}
}

// rejectDistQueue and rejectDistPrioQueue reject every EnqueueOnce.
type rejectDistQueue struct{ *mocks.MockDistributedQueue }

func (q *rejectDistQueue) EnqueueOnce(any, string) bool { return false }

type rejectDistPrioQueue struct {
	*mocks.MockDistributedPriorityQueue
}

func (q *rejectDistPrioQueue) EnqueueOnce(any, int, string) bool { return false }

// An Add whose EnqueueOnce the storage rejects reports failure.
func TestAddWithIdFailsWhenEnqueueOnceFails(t *testing.T) {
	noop := func(Job[string]) {}
	p := NewWorker(noop).WithPersistentQueue(&rejectDistQueue{mocks.NewMockDistributedQueue()})
	defer p.Worker().Stop()
	pp := NewWorker(noop).WithPersistentPriorityQueue(&rejectDistPrioQueue{mocks.NewMockDistributedPriorityQueue()})
	defer pp.Worker().Stop()
	d := NewDistributedQueue[string](&rejectDistQueue{mocks.NewMockDistributedQueue()})
	dp := NewDistributedPriorityQueue[string](&rejectDistPrioQueue{mocks.NewMockDistributedPriorityQueue()})

	assert.False(t, p.Add("x", WithJobId("a")), "persistent")
	assert.False(t, pp.Add("x", 1, WithJobId("a")), "persistent priority")
	assert.False(t, d.Add("x", WithJobId("a")), "distributed")
	assert.False(t, dp.Add("x", 1, WithJobId("a")), "distributed priority")
	assert.Zero(t, p.Worker().Metrics().Submitted(), "nothing was submitted")
}

// Persistent and distributed Adds check generated ids too: they guard the
// storage client's re-send of a push, which repeats the id.
func TestGeneratedIdUsesEnqueueOnceOnStorage(t *testing.T) {
	s := &dedupDistQueue{MockDistributedQueue: mocks.NewMockDistributedQueue()}
	q := NewWorker(func(Job[string]) {}, WithAutoRun(false), WithJobIdGenerator(func() string { return "g1" })).WithPersistentQueue(s)

	assert.True(t, q.Add("x"))
	assert.True(t, q.Add("x"))

	s.log.mu.Lock()
	defer s.log.mu.Unlock()
	assert.Equal(t, []string{"g1", "g1"}, s.log.onceID)
	assert.Equal(t, 1, s.log.stored)
}
