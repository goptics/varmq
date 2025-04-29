package varmq

import (
	"time"

	"github.com/fahimfaisaal/varmq/utils"
)

type ConfigFunc func(*configs)

type configs struct {
	Concurrency          uint32
	Cache                ICache
	CleanupCacheInterval time.Duration
	JobIdGenerator       func() string
}

func newConfig() configs {
	return configs{
		Concurrency: 1,
		Cache:       getCache(),
		JobIdGenerator: func() string {
			return ""
		},
	}
}

func loadConfigs(config ...any) configs {
	c := newConfig()

	return mergeConfigs(c, config...)
}

func mergeConfigs(c configs, cs ...any) configs {
	for _, config := range cs {
		switch config := config.(type) {
		case ConfigFunc:
			config(&c)
		case int:
			c.Concurrency = withSafeConcurrency(config)
		}
	}

	return c
}

func WithCache(cache ICache) ConfigFunc {
	return func(c *configs) {
		c.Cache = cache
	}
}

func WithConcurrency(concurrency int) ConfigFunc {
	return func(c *configs) {
		c.Concurrency = withSafeConcurrency(concurrency)
	}
}

func WithAutoCleanupCache(duration time.Duration) ConfigFunc {
	return func(c *configs) {
		c.CleanupCacheInterval = duration
	}
}

func WithJobIdGenerator(fn func() string) ConfigFunc {
	return func(c *configs) {
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
