package varmq

import (
	"context"
	"testing"
	"time"

	"github.com/goptics/varmq/utils"
	"github.com/stretchr/testify/assert"
)

func TestConfig(t *testing.T) {
	t.Run("NewConfig", func(t *testing.T) {
		c := newConfig()

		// Test default values
		assert.Equal(t, uint32(1), c.concurrency)
		assert.NotNil(t, c.jobIdGenerator)
		assert.Equal(t, "", c.jobIdGenerator())
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

					assert.Equal(t, tc.expected, c.concurrency)
				})
			}
		})

		t.Run("WithStrategy", func(t *testing.T) {
			tests := []struct {
				name             string
				strategy         Strategy
				expectedStrategy Strategy
			}{
				{"RoundRobin Strategy", RoundRobin, RoundRobin},
				{"MaxLen Strategy", MaxLen, MaxLen},
				{"MinLen Strategy", MinLen, MinLen},
			}

			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					configFunc := WithStrategy(tc.strategy)
					c := newConfig()
					configFunc(&c)

					assert.Equal(t, tc.expectedStrategy, c.strategy)
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

			assert.Equal(t, expectedId, c.jobIdGenerator())
		})

		t.Run("WithIdleWorkerExpiryDuration", func(t *testing.T) {
			duration := 10 * time.Minute
			configFunc := WithIdleWorkerExpiryDuration(duration)

			c := newConfig()
			configFunc(&c)

			assert.Equal(t, duration, c.idleWorkerExpiryDuration)
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

					assert.Equal(t, tc.expectedRatio, c.minIdleWorkerRatio)
				})
			}
		})
	})

	t.Run("ConfigManagement", func(t *testing.T) {
		t.Run("LoadConfigs", func(t *testing.T) {
			// Test with no configs
			c := loadConfigs()
			assert.Equal(t, uint32(1), c.concurrency)

			// Test with concurrency as int
			c = loadConfigs(5)
			assert.Equal(t, uint32(5), c.concurrency)

			// Test with multiple config funcs
			expectedId := "custom-id"

			c = loadConfigs(
				WithConcurrency(3),
				WithJobIdGenerator(func() string { return expectedId }),
			)

			assert.Equal(t, uint32(3), c.concurrency)
			assert.Equal(t, expectedId, c.jobIdGenerator())

			// Test with a mixture of int and config funcs
			c = loadConfigs(4)

			assert.Equal(t, uint32(4), c.concurrency)
		})

		t.Run("MergeConfigs", func(t *testing.T) {
			baseConfig := configs{
				concurrency:    1,
				jobIdGenerator: func() string { return "" },
			}

			// Test with no changes
			c := mergeConfigs(baseConfig)
			assert.Equal(t, baseConfig.concurrency, c.concurrency)

			// Test with concurrency as int
			c = mergeConfigs(baseConfig, 5)
			assert.Equal(t, uint32(5), c.concurrency)

			// Test with config funcs
			c = mergeConfigs(
				baseConfig,
				WithConcurrency(3),
			)

			assert.Equal(t, uint32(3), c.concurrency)
		})
	})

	t.Run("JobConfigs", func(t *testing.T) {
		t.Run("loadJobConfigs", func(t *testing.T) {
			// Test with default job ID generator
			qConfig := configs{
				jobIdGenerator: func() string { return "default-id" },
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

func TestClampPercentage(t *testing.T) {
	tests := []struct {
		name     string
		input    uint8
		expected uint8
	}{
		{"Zero should return 1", 0, 1},
		{"Valid percentage should return same value", 50, 50},
		{"Min valid value should return same", 1, 1},
		{"Max valid value should return same", 100, 100},
		{"Over 100 should return 100", 150, 100},
		{"Just over 100 should return 100", 101, 100},
		{"Max uint8 should return 100", 255, 100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := clampPercentage(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	original := defaultConfig
	defer func() { defaultConfig = original }()

	t.Run("DefaultConcurrency", func(t *testing.T) {
		tests := []struct {
			name        string
			concurrency int
			expected    uint32
		}{
			{"Positive value sets concurrency", 5, 5},
			{"Large value sets concurrency", 100, 100},
			{"One sets concurrency", 1, 1},
			{"Zero uses CPU count", 0, utils.Cpus()},
			{"Negative uses CPU count", -1, utils.Cpus()},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				defaultConfig = original
				DefaultConcurrency(tc.concurrency)
				assert.Equal(t, tc.expected, defaultConfig.concurrency)
			})
		}
	})

	t.Run("DefaultStrategy", func(t *testing.T) {
		tests := []struct {
			name     string
			strategy Strategy
		}{
			{"Sets RoundRobin", RoundRobin},
			{"Sets MaxLen", MaxLen},
			{"Sets MinLen", MinLen},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				defaultConfig = original
				DefaultStrategy(tc.strategy)
				assert.Equal(t, tc.strategy, defaultConfig.strategy)
			})
		}
	})

	t.Run("DefaultMinIdleWorkRatio", func(t *testing.T) {
		tests := []struct {
			name       string
			percentage uint8
			expected   uint8
		}{
			{"Valid percentage is set", 50, 50},
			{"Min valid value", 1, 1},
			{"Max valid value", 100, 100},
			{"Zero is clamped to 1", 0, 1},
			{"Over 100 is clamped to 100", 150, 100},
			{"Max uint8 is clamped to 100", 255, 100},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				defaultConfig = original
				DefaultMinIdleWorkRatio(tc.percentage)
				assert.Equal(t, tc.expected, defaultConfig.minIdleWorkerRatio)
			})
		}
	})

	t.Run("DefaultJobIdGenerator", func(t *testing.T) {
		t.Run("Sets custom generator", func(t *testing.T) {
			defaultConfig = original
			generator := func() string { return "custom-id" }
			DefaultJobIdGenerator(generator)
			assert.Equal(t, "custom-id", defaultConfig.jobIdGenerator())
		})

		t.Run("Sets generator returning empty string", func(t *testing.T) {
			defaultConfig = original
			generator := func() string { return "" }
			DefaultJobIdGenerator(generator)
			assert.Equal(t, "", defaultConfig.jobIdGenerator())
		})

		t.Run("Sets generator with counter", func(t *testing.T) {
			defaultConfig = original
			counter := 0
			generator := func() string {
				counter++
				return "id-" + string(rune('0'+counter))
			}
			DefaultJobIdGenerator(generator)
			first := defaultConfig.jobIdGenerator()
			second := defaultConfig.jobIdGenerator()
			assert.NotEqual(t, first, second, "generator should produce unique IDs")
		})
	})

	t.Run("DefaultIdleWorkerExpiryDuration", func(t *testing.T) {
		tests := []struct {
			name     string
			duration time.Duration
		}{
			{"Sets seconds duration", 30 * time.Second},
			{"Sets minute duration", 5 * time.Minute},
			{"Sets zero duration", 0},
			{"Sets large duration", 24 * time.Hour},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				defaultConfig = original
				DefaultIdleWorkerExpiryDuration(tc.duration)
				assert.Equal(t, tc.duration, defaultConfig.idleWorkerExpiryDuration)
			})
		}
	})

	t.Run("DefaultCtx", func(t *testing.T) {
		t.Run("Sets background context", func(t *testing.T) {
			defaultConfig = original
			ctx := context.Background()
			DefaultCtx(ctx)
			assert.Equal(t, ctx, defaultConfig.ctx)
		})

		t.Run("Sets context with cancel", func(t *testing.T) {
			defaultConfig = original
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			DefaultCtx(ctx)
			assert.Equal(t, ctx, defaultConfig.ctx)
		})

		t.Run("Sets context with timeout", func(t *testing.T) {
			defaultConfig = original
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			DefaultCtx(ctx)
			assert.Equal(t, ctx, defaultConfig.ctx)
		})

		t.Run("Sets TODO context", func(t *testing.T) {
			defaultConfig = original
			DefaultCtx(context.TODO())
			assert.NotNil(t, defaultConfig.ctx)
		})
	})
}
