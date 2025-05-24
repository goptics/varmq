package varmq

import (
	"testing"
	"time"

	"github.com/goptics/varmq/utils"
	"github.com/stretchr/testify/assert"
)

func TestConfig(t *testing.T) {
	t.Run("NewConfig", func(t *testing.T) {
		c := newConfig()

		// Test default values
		assert.Equal(t, uint32(1), c.Concurrency)
		assert.Equal(t, time.Duration(0), c.CleanupCacheInterval)
		assert.NotNil(t, c.JobIdGenerator)
		assert.Equal(t, "", c.JobIdGenerator())
	})

	t.Run("ConfigOptions", func(t *testing.T) {
		t.Run("WithConcurrency", func(t *testing.T) {
			tests := []struct {
				name        string
				concurrency int
				expected    uint32
			}{
				{"Zero concurrency should use CPU count", 0, utils.Cpus()},
				{"Negative concurrency should use CPU count", -1, utils.Cpus()},
				{"Positive concurrency should use provided value", 5, 5},
			}

			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					configFunc := WithConcurrency(tc.concurrency)
					c := newConfig()
					configFunc(&c)

					assert.Equal(t, tc.expected, c.Concurrency)
				})
			}
		})

		t.Run("WithSafeConcurrency", func(t *testing.T) {
			tests := []struct {
				name        string
				concurrency int
				expected    uint32
			}{
				{"Zero concurrency should use CPU count", 0, utils.Cpus()},
				{"Negative concurrency should use CPU count", -1, utils.Cpus()},
				{"Positive concurrency should use provided value", 5, 5},
			}

			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					result := withSafeConcurrency(tc.concurrency)
					assert.Equal(t, tc.expected, result)
				})
			}
		})

		t.Run("WithAutoCleanupCache", func(t *testing.T) {
			duration := 5 * time.Minute
			configFunc := WithAutoCleanupCache(duration)

			c := newConfig()
			configFunc(&c)

			assert.Equal(t, duration, c.CleanupCacheInterval)
		})

		t.Run("WithJobIdGenerator", func(t *testing.T) {
			expectedId := "test-job-id"
			generator := func() string {
				return expectedId
			}

			configFunc := WithJobIdGenerator(generator)
			c := newConfig()
			configFunc(&c)

			assert.Equal(t, expectedId, c.JobIdGenerator())
		})

		t.Run("WithIdleWorkerExpiryDuration", func(t *testing.T) {
			duration := 10 * time.Minute
			configFunc := WithIdleWorkerExpiryDuration(duration)

			c := newConfig()
			configFunc(&c)

			assert.Equal(t, duration, c.IdleWorkerExpiryDuration)
		})

		t.Run("WithMinIdleWorkerRatio", func(t *testing.T) {
			tests := []struct {
				name          string
				percentage    uint8
				expectedRatio uint8
			}{
				{"Zero percentage should be clamped to 1", 0, 1},
				{"Value above 100 should be clamped to 100", 150, 100},
				{"Value within range should remain unchanged", 20, 20},
				{"Minimum valid value", 1, 1},
				{"Maximum valid value", 100, 100},
			}

			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					configFunc := WithMinIdleWorkerRatio(tc.percentage)
					c := newConfig()
					configFunc(&c)

					assert.Equal(t, tc.expectedRatio, c.MinIdleWorkerRatio)
				})
			}
		})
	})

	t.Run("ConfigManagement", func(t *testing.T) {
		t.Run("LoadConfigs", func(t *testing.T) {
			// Test with no configs
			c := loadConfigs()
			assert.Equal(t, uint32(1), c.Concurrency)

			// Test with concurrency as int
			c = loadConfigs(5)
			assert.Equal(t, uint32(5), c.Concurrency)

			// Test with multiple config funcs
			duration := 10 * time.Minute
			expectedId := "custom-id"

			c = loadConfigs(
				WithConcurrency(3),
				WithAutoCleanupCache(duration),
				WithJobIdGenerator(func() string { return expectedId }),
			)

			assert.Equal(t, uint32(3), c.Concurrency)
			assert.Equal(t, duration, c.CleanupCacheInterval)
			assert.Equal(t, expectedId, c.JobIdGenerator())

			// Test with a mixture of int and config funcs
			c = loadConfigs(
				4,
			)

			assert.Equal(t, uint32(4), c.Concurrency)
		})

		t.Run("MergeConfigs", func(t *testing.T) {
			baseConfig := configs{
				Concurrency:          1,
				CleanupCacheInterval: 0,
				JobIdGenerator:       func() string { return "" },
			}

			// Test with no changes
			c := mergeConfigs(baseConfig)
			assert.Equal(t, baseConfig.Concurrency, c.Concurrency)

			// Test with concurrency as int
			c = mergeConfigs(baseConfig, 5)
			assert.Equal(t, uint32(5), c.Concurrency)

			// Test with config funcs
			c = mergeConfigs(
				baseConfig,
				WithConcurrency(3),
			)

			assert.Equal(t, uint32(3), c.Concurrency)
		})
	})
}
