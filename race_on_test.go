//go:build race

package varmq

// raceEnabled: the race detector adds allocations, so alloc counts skip.
const raceEnabled = true
