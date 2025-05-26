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
			expectedId := "custom-id"

			c = loadConfigs(
				WithConcurrency(3),
				WithJobIdGenerator(func() string { return expectedId }),
			)

			assert.Equal(t, uint32(3), c.Concurrency)
			assert.Equal(t, expectedId, c.JobIdGenerator())

			// Test with a mixture of int and config funcs
			c = loadConfigs(
				4,
			)

			assert.Equal(t, uint32(4), c.Concurrency)
		})

		t.Run("MergeConfigs", func(t *testing.T) {
			baseConfig := configs{
				Concurrency:    1,
				JobIdGenerator: func() string { return "" },
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

	t.Run("JobConfigs", func(t *testing.T) {
		t.Run("loadJobConfigs", func(t *testing.T) {
			// Test with default job ID generator
			qConfig := configs{
				JobIdGenerator: func() string { return "default-id" },
			}

			// Test with no custom configs
			jc := loadJobConfigs(qConfig)
			assert.Equal(t, "default-id", jc.Id)

			// Test with custom job ID
			jc = loadJobConfigs(qConfig, WithJobId("custom-id"))
			assert.Equal(t, "custom-id", jc.Id)

			// Test with multiple configs (should apply in order)
			jc = loadJobConfigs(qConfig,
				WithJobId("first-id"),
				WithJobId("second-id"),
			)
			assert.Equal(t, "second-id", jc.Id)
		})

		t.Run("WithJobId", func(t *testing.T) {
			// Test with non-empty ID
			configFunc := WithJobId("test-id")
			jc := jobConfigs{Id: "original-id"}
			configFunc(&jc)
			assert.Equal(t, "test-id", jc.Id)

			// Test with empty ID (should not change the original ID)
			configFunc = WithJobId("")
			jc = jobConfigs{Id: "original-id"}
			configFunc(&jc)
			assert.Equal(t, "original-id", jc.Id)
		})
	})
}
