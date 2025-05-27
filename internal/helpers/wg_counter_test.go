package helpers

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWgCounter(t *testing.T) {
	t.Run("Creation", func(t *testing.T) {
		assert := assert.New(t)

		// Test with zero buffer size
		wgc0 := NewWgCounter(0)
		assert.NotNil(wgc0, "WgCounter with zero buffer size should not be nil")
		assert.Equal(0, wgc0.Count(), "WgCounter with zero buffer size should have count 0")

		// Test with positive buffer size
		wgc := NewWgCounter(5)
		assert.NotNil(wgc, "WgCounter should not be nil")
		assert.Equal(5, wgc.Count(), "WgCounter should initialize with the provided buffer size")
	})

	t.Run("Count", func(t *testing.T) {
		assert := assert.New(t)
		wgc := NewWgCounter(3)

		assert.Equal(3, wgc.Count(), "Initial count should match buffer size")

		wgc.Done()
		assert.Equal(2, wgc.Count(), "Count should decrease after Done")

		wgc.Done()
		assert.Equal(1, wgc.Count(), "Count should decrease after Done")

		wgc.Done()
		assert.Equal(0, wgc.Count(), "Count should be zero after all Done calls")
	})

	t.Run("Done Safety", func(t *testing.T) {
		assert := assert.New(t)
		wgc := NewWgCounter(1)

		wgc.Done()
		assert.Equal(0, wgc.Count(), "Count should be zero after Done")

		// Calling Done after count is zero should not panic
		wgc.Done()
		assert.Equal(0, wgc.Count(), "Count should remain zero after extra Done calls")

		// Multiple extra Done calls should be safe
		wgc.Done()
		wgc.Done()
		assert.Equal(0, wgc.Count(), "Count should remain zero after multiple extra Done calls")
	})

	t.Run("Wait", func(t *testing.T) {
		assert := assert.New(t)
		wgc := NewWgCounter(2)

		// Set up a channel to signal when Wait completes
		done := make(chan struct{})

		// Start a goroutine that calls Wait
		go func() {
			wgc.Wait()
			close(done)
		}()

		// Wait should not complete immediately
		select {
		case <-done:
			assert.Fail("Wait should not complete before Done calls")
		case <-time.After(10 * time.Millisecond):
			// This is expected
		}

		// Call Done once
		wgc.Done()

		// Wait should still not complete
		select {
		case <-done:
			assert.Fail("Wait should not complete before all Done calls")
		case <-time.After(10 * time.Millisecond):
			// This is expected
		}

		// Call Done again to reach zero
		wgc.Done()

		// Now Wait should complete
		select {
		case <-done:
			// This is expected
		case <-time.After(100 * time.Millisecond):
			assert.Fail("Wait should complete after all Done calls")
		}
	})

	t.Run("Concurrent Usage", func(t *testing.T) {
		assert := assert.New(t)
		numGoroutines := 10
		wgc := NewWgCounter(numGoroutines)

		// Use a mutex to protect access to the counter
		var mu sync.Mutex
		counter := 0

		// Start multiple goroutines that call Done
		var wg sync.WaitGroup
		wg.Add(numGoroutines)
		for range numGoroutines {
			go func() {
				defer wg.Done()

				// Simulate some work
				time.Sleep(time.Duration(5) * time.Millisecond)

				// Increment counter and call Done
				mu.Lock()
				counter++
				mu.Unlock()

				wgc.Done()
			}()
		}

		// Wait for all goroutines to complete
		wgc.Wait()

		// Verify all goroutines ran
		assert.Equal(numGoroutines, counter, "All goroutines should have run")
		assert.Equal(0, wgc.Count(), "Count should be zero after all Done calls")

		// Wait for our test goroutines to complete
		wg.Wait()
	})
}
