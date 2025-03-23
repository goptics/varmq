package gocq

import (
	"time"

	"github.com/fahimfaisaal/gocq/v2/shared/utils"
)

type Config func(*Configs)

type Configs struct {
	Concurrency          uint32
	Cache                Cache
	CleanupCacheInterval time.Duration
	JobIdGenerator       func() string
}

func loadConfigs(configs ...any) Configs {
	c := Configs{
		Concurrency: withSafeConcurrency(0),
		Cache:       getCache(),
	}

	return mergeConfigs(c, configs...)
}

func mergeConfigs(c Configs, cs ...any) Configs {
	for _, config := range cs {
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

func WithAutoCleanupCache(duration time.Duration) Config {
	return func(c *Configs) {
		c.CleanupCacheInterval = duration
	}
}

func WithJobIdGenerator(fn func() string) Config {
	return func(c *Configs) {
		c.JobIdGenerator = fn
	}
}

func withSafeConcurrency(concurrency int) uint32 {
	// If concurrency is less than 1, use the number of CPUs as the concurrency
	if concurrency < 1 {
		return utils.Cpus()
	}
	return uint32(concurrency)
}
