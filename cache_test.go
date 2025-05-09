package varmq

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCache(t *testing.T) {
	assert := assert.New(t)

	// defaultCache should be nil at the start
	assert.Nil(defaultCache, "defaultCache should be nil at the start")

	// First call should initialize a new nullCache
	cache1 := getCache()
	assert.NotNil(cache1, "getCache() should return a non-nil cache")

	// Check the type is nullCache
	_, ok := cache1.(*nullCache)
	assert.True(ok, "getCache() should return a *nullCache, got %T", cache1)

	// Second call should return the same instance
	cache2 := getCache()
	assert.NotNil(cache2, "second call to getCache() should return a non-nil cache")

	// Both calls should return the same instance
	assert.Same(cache1, cache2, "getCache() should return the same instance on multiple calls")

	// Test the global variable was properly set
	assert.NotNil(defaultCache, "defaultCache should be initialized")

	// Test cache functionality is correctly implemented
	// The nullCache should return nil,false for Load
	val, ok := cache1.Load("any_key")
	assert.False(ok, "Load() should return false for any key")
	assert.Nil(val, "Load() should return nil for any key")
}

func TestGetCacheSingleton(t *testing.T) {
	assert := assert.New(t)
	
	// This test verifies the singleton pattern works across multiple calls
	cache1 := getCache()
	cache2 := getCache()

	// Check that the same instance is returned every time
	assert.Same(cache1, cache2, "getCache() should return the same instance on multiple calls")
}
