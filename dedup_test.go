package varmq

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/goptics/varmq/mocks"
	"github.com/stretchr/testify/assert"
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
