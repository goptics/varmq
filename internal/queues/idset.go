package queues

import (
	"hash/maphash"
	"sync"
)

// idMap is a set of ids. It is not safe for concurrent use.
type idMap struct {
	m    map[string]struct{}
	peak int // most ids held since m was made
}

// idsShrinkPeak is the set size above which a drained set is dropped. A map
// keeps its groups after its keys are deleted, so a burst of ids would hold
// its peak memory; a small set is kept, sparing an allocation per refill.
const idsShrinkPeak = 1024

// idShardShrinkPeak is idsShrinkPeak for one of an idSet's idShards sets.
const idShardShrinkPeak = idsShrinkPeak / idShards

// add stores id and reports whether it was new: one probe, since an assign
// of a stored key leaves the length as it was.
func (s *idMap) add(id string) bool {
	if s.m == nil {
		s.m = make(map[string]struct{})
	}

	n := len(s.m)
	s.m[id] = struct{}{}
	if len(s.m) == n {
		return false
	}

	s.peak = max(s.peak, n+1)

	return true
}

// remove deletes id, and drops the set once a burst drains.
func (s *idMap) remove(id string) {
	delete(s.m, id)
	if len(s.m) == 0 && s.peak > idShardShrinkPeak {
		s.m, s.peak = nil, 0
	}
}

// idShards is how many locks an idSet spreads ids over.
// ponytail: fixed 64 (4 KiB a set); size it from GOMAXPROCS if more cores
// still contend.
const idShards = 64

// idShard is one lock and its ids, alone on a 64-byte line.
type idShard struct {
	mu  sync.Mutex
	ids idMap
	_   [64 - 8 - 16]byte
}

// idSet is an idMap safe for concurrent use. An id's lock is picked by its
// hash, so adds of different ids rarely wait on each other.
type idSet struct {
	seed   maphash.Seed
	shards [idShards]idShard
}

func newIdSet() *idSet {
	return &idSet{seed: maphash.MakeSeed()}
}

func (s *idSet) shard(id string) *idShard {
	return &s.shards[maphash.String(s.seed, id)&(idShards-1)]
}

func (s *idSet) remove(id string) {
	sh := s.shard(id)
	sh.mu.Lock()
	sh.ids.remove(id)
	sh.mu.Unlock()
}

// len counts the ids in every shard, one lock at a time.
func (s *idSet) len() int {
	n := 0
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		n += len(sh.ids.m)
		sh.mu.Unlock()
	}

	return n
}
