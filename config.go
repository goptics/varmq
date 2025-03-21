package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/internal/job"
	"github.com/fahimfaisaal/gocq/v2/internal/queue"
	"github.com/fahimfaisaal/gocq/v2/shared/types"
	"github.com/fahimfaisaal/gocq/v2/shared/utils"
)

type Config func(*Configs)

type Configs struct {
	Concurrency   uint32
	Queue         types.IQueue
	Cache         Cache
	IsDistributed bool
}

func loadConfigs[T, R any](configs ...Config) Configs {
	c := Configs{
		Concurrency: withSafeConcurrency(0),
		Queue:       queue.NewQueue[job.Job[T, R]](),
		Cache:       getCache(),
	}

	for _, config := range configs {
		config(&c)
	}

	return c
}

func WithQueue(queue types.IQueue) Config {
	return func(c *Configs) {
		c.Queue = queue
	}
}

func WithCache(cache Cache) Config {
	return func(c *Configs) {
		c.Cache = cache
	}
}

func WithConcurrency(concurrency uint32) Config {
	return func(c *Configs) {
		c.Concurrency = withSafeConcurrency(concurrency)
	}
}

func WithDistribution(enabled bool) Config {
	return func(c *Configs) {
		c.IsDistributed = enabled
	}
}

func withSafeConcurrency(concurrency uint32) uint32 {
	// If concurrency is less than 1, use the number of CPUs as the concurrency
	if concurrency < 1 {
		return utils.Cpus()
	}
	return concurrency
}
