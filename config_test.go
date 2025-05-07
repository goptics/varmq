package varmq

import (
	"testing"
	"time"

	"github.com/goptics/varmq/utils"
	"github.com/stretchr/testify/assert"
)

func TestNewConfig(t *testing.T) {
	c := newConfig()

	// Test default values
	assert.Equal(t, uint32(1), c.Concurrency)
	assert.NotNil(t, c.Cache)
	assert.Equal(t, time.Duration(0), c.CleanupCacheInterval)
	assert.NotNil(t, c.JobIdGenerator)
	assert.Equal(t, "", c.JobIdGenerator())
}

func TestWithCache(t *testing.T) {
	mockCache := getCache()
	configFunc := WithCache(mockCache)

	c := newConfig()
	configFunc(&c)

	assert.Equal(t, mockCache, c.Cache)
}

func TestWithConcurrency(t *testing.T) {
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
}

func TestWithSafeConcurrency(t *testing.T) {
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
}

func TestWithAutoCleanupCache(t *testing.T) {
	duration := 5 * time.Minute
	configFunc := WithAutoCleanupCache(duration)

	c := newConfig()
	configFunc(&c)

	assert.Equal(t, duration, c.CleanupCacheInterval)
}

func TestWithJobIdGenerator(t *testing.T) {
	expectedId := "test-job-id"
	generator := func() string {
		return expectedId
	}

	configFunc := WithJobIdGenerator(generator)
	c := newConfig()
	configFunc(&c)

	assert.Equal(t, expectedId, c.JobIdGenerator())
}

func TestLoadConfigs(t *testing.T) {
	// Test with no configs
	c := loadConfigs()
	assert.Equal(t, uint32(1), c.Concurrency)

	// Test with concurrency as int
	c = loadConfigs(5)
	assert.Equal(t, uint32(5), c.Concurrency)

	// Test with multiple config funcs
	mockCache := getCache()
	duration := 10 * time.Minute
	expectedId := "custom-id"

	c = loadConfigs(
		WithConcurrency(3),
		WithCache(mockCache),
		WithAutoCleanupCache(duration),
		WithJobIdGenerator(func() string { return expectedId }),
	)

	assert.Equal(t, uint32(3), c.Concurrency)
	assert.Equal(t, mockCache, c.Cache)
	assert.Equal(t, duration, c.CleanupCacheInterval)
	assert.Equal(t, expectedId, c.JobIdGenerator())

	// Test with a mixture of int and config funcs
	c = loadConfigs(
		4,
		WithCache(mockCache),
	)

	assert.Equal(t, uint32(4), c.Concurrency)
	assert.Equal(t, mockCache, c.Cache)
}

func TestMergeConfigs(t *testing.T) {
	baseConfig := configs{
		Concurrency:          1,
		Cache:                getCache(),
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
	newCache := getCache()
	c = mergeConfigs(
		baseConfig,
		WithCache(newCache),
		WithConcurrency(3),
	)

	assert.Equal(t, uint32(3), c.Concurrency)
	assert.Equal(t, newCache, c.Cache)
}
