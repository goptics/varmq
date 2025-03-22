package gocq

import (
	"github.com/fahimfaisaal/gocq/v2/shared/utils"
)

type Config func(*Configs)

type Configs struct {
	Concurrency uint32
	Cache       Cache
}

func loadConfigs(configs ...any) Configs {
	c := Configs{
		Concurrency: withSafeConcurrency(0),
		Cache:       getCache(),
	}

	for _, config := range configs {
		switch config := config.(type) {
		case Config:
			config(&c)
		case int:
			c.Concurrency = withSafeConcurrency(config)
		}
	}

	return c
}

func WithCache(cache Cache) Config {
	return func(c *Configs) {
		c.Cache = cache
	}
}

func WithConcurrency(concurrency int) Config {
	return func(c *Configs) {
		c.Concurrency = withSafeConcurrency(concurrency)
	}
}

func withSafeConcurrency(concurrency int) uint32 {
	// If concurrency is less than 1, use the number of CPUs as the concurrency
	if concurrency < 1 {
		return utils.Cpus()
	}
	return uint32(concurrency)
}
