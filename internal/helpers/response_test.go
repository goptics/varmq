package helpers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResponse(t *testing.T) {
	t.Run("Creation", func(t *testing.T) {
		assert := assert.New(t)

		// Test with zero capacity
		r0 := NewResponse[int](0)
		assert.NotNil(r0, "response with zero capacity should not be nil")

		// Test with non-zero capacity
		r := NewResponse[string](5)
		assert.NotNil(r, "response should not be nil")
	})

	t.Run("Send and Read", func(t *testing.T) {
		assert := assert.New(t)
		r := NewResponse[int](1)

		// Get read channel
		ch := r.Read()
		assert.NotNil(ch, "read channel should not be nil")

		// Send a value
		go r.Send(42)

		// Read the value
		val := <-ch
		assert.Equal(42, val, "should receive the sent value")
	})

	t.Run("Response Method", func(t *testing.T) {
		assert := assert.New(t)
		r := NewResponse[string](1)

		// Send a value
		go r.Send("hello")

		// Get the response
		val, err := r.Response()
		assert.NoError(err, "response should not return error")
		assert.Equal("hello", val, "should receive the sent value")

		// Test after closing
		r.Close()

		// Response should still return the last value after closing
		val, err = r.Response()
		assert.NoError(err, "response should not return error after closing")
		assert.Equal("hello", val, "should receive the last stored value after closing")
	})

	t.Run("Drain", func(t *testing.T) {
		assert := assert.New(t)
		r := NewResponse[int](5)

		// Send multiple values
		for i := range 3 {
			r.Send(i)
		}

		// Capture the channel before draining
		ch := r.Read()

		// Read one value to verify channel has data
		val1 := <-ch
		assert.Equal(0, val1, "first value should be 0")

		// Drain the channel
		r.Drain()

		// Close the channel to ensure the drain goroutine completes
		r.Close()

		// Allow time for drain goroutine to work
		time.Sleep(10 * time.Millisecond)

		// The channel should be drained and closed
		_, ok := <-ch
		assert.False(ok, "channel should be closed after draining")
	})

	t.Run("Close", func(t *testing.T) {
		assert := assert.New(t)
		r := NewResponse[int](1)

		// Send a value
		r.Send(42)

		// Close the channel
		err := r.Close()
		assert.NoError(err, "close should not return error")

		// Try to read after closing
		val, err := r.Response()
		assert.NoError(err, "response should not return error after closing")
		assert.Equal(42, val, "should receive the stored value after closing")

		// Reading from the channel directly should return zero value and false
		_, ok := <-r.Read()
		assert.False(ok, "reading from closed channel should return false")
	})

	t.Run("Concurrent Operations", func(t *testing.T) {
		assert := assert.New(t)
		r := NewResponse[int](10)

		// Send values concurrently
		go func() {
			for i := 0; i < 5; i++ {
				r.Send(i)
			}
		}()

		// Read values concurrently
		var received []int
		for i := 0; i < 5; i++ {
			val, err := r.Response()
			assert.NoError(err, "response should not return error")
			received = append(received, val)
		}

		assert.Len(received, 5, "should receive all sent values")
	})
}
