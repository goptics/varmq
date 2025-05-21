package varmq

import "time"

type poolNode[T, R any] struct {
	ch       chan iJob[T, R]
	lastUsed time.Time
}

func (wc *poolNode[T, R]) Close() {
	close(wc.ch)
}

func (wc *poolNode[T, R]) UpdateLastUsed() {
	wc.lastUsed = time.Now()
}
