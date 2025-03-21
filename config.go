package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/shared/utils"
)

type Config func(*Configs)

type Configs struct {
	Concurrency uint32
	Cache       Cache
}

func loadConfigs[T, R any](configs ...Config) Configs {
	c := Configs{
		Concurrency: withSafeConcurrency(0),
		Cache:       getCache(),
	}

	for _, config := range configs {
		config(&c)
	}

	return c
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

func withSafeConcurrency(concurrency uint32) uint32 {
	// If concurrency is less than 1, use the number of CPUs as the concurrency
	if concurrency < 1 {
		return utils.Cpus()
	}
	return concurrency
}
