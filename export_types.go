package gocq

import (
	cq "github.com/fahimfaisaal/gocq/internal/concurrent_queue"
	vq "github.com/fahimfaisaal/gocq/internal/concurrent_queue/void_queue"
	"github.com/fahimfaisaal/gocq/internal/shared"
)

type PQItem[T any] cq.PQItem[T]

type ConcurrentQueue[T, R any] cq.IConcurrentQueue[T, R]

type ConcurrentVoidQueue[T any] vq.IConcurrentVoidQueue[T]

type ConcurrentPriorityQueue[T, R any] cq.IConcurrentPriorityQueue[T, R]

type ConcurrentVoidPriorityQueue[T any] vq.IConcurrentVoidPriorityQueue[T]

type Result[T any] shared.Result[T]

type EnqueuedJob[T any] cq.EnqueuedJob[T]

type EnqueuedVoidJob cq.EnqueuedVoidJob

type EnqueuedGroupJob[T any] cq.EnqueuedGroupJob[T]

type EnqueuedVoidGroupJob cq.EnqueuedVoidGroupJob
