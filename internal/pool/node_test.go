package pool

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPoolNode(t *testing.T) {
	t.Run("Initialization", func(t *testing.T) {
		assert := assert.New(t)

		// Create a new poolNode using the factory function
		node := CreateNode[string](1)

		// Check initial lastUsed time is zero
		assert.True(node.GetLastUsed().IsZero(), "lastUsed time should be zero for a newly created node")

		// Check channel is initialized
		assert.NotNil(node.ch, "channel should be initialized")
	})

	t.Run("UpdateLastUsed", func(t *testing.T) {
		assert := assert.New(t)

		// Create a new poolNode using the factory function
		node := CreateNode[string](1)

		// Initial time should be zero
		assert.True(node.GetLastUsed().IsZero(), "lastUsed time should initially be zero")

		// Update the last used time
		before := time.Now()
		node.UpdateLastUsed()
		after := time.Now()

		// Verify lastUsed time was updated and is between before and after
		assert.False(node.GetLastUsed().IsZero(), "lastUsed time should no longer be zero")
		assert.True(node.GetLastUsed().After(before) || node.GetLastUsed().Equal(before),
			"lastUsed time should be after or equal to time before update")
		assert.True(node.GetLastUsed().Before(after) || node.GetLastUsed().Equal(after),
			"lastUsed time should be before or equal to time after update")
	})

	t.Run("GetLastUsed", func(t *testing.T) {
		assert := assert.New(t)

		// Create a new poolNode using the factory function
		node := CreateNode[string](1)

		// Initial time should be zero
		initialTime := node.GetLastUsed()
		assert.True(initialTime.IsZero(), "initial time should be zero")

		// Update the time
		node.UpdateLastUsed()
		updatedTime := node.GetLastUsed()
		assert.False(updatedTime.IsZero(), "updated time should not be zero")
		assert.True(updatedTime.After(initialTime), "updated time should be after initial time")
	})

	t.Run("Stop", func(t *testing.T) {
		assert := assert.New(t)

		// Create a new poolNode using the factory function
		node := CreateNode[string](1)

		// Call Stop to send the sentinel payload
		node.Stop()

		// Expect to receive the sentinel payload with ok=false
		sentinel := <-node.ch
		assert.False(sentinel.ok, "expected sentinel payload with ok=false after Stop() call")
	})

	t.Run("Read", func(t *testing.T) {
		assert := assert.New(t)

		// Create a new node with buffer size 1
		node := CreateNode[string](1)

		// Send a value using the Send method
		expectedValue := "test-value"
		node.Send(expectedValue)

		// Verify we can read the payload through the returned channel
		payload := <-node.ch
		assert.True(payload.ok, "payload ok flag should be true")
		assert.Equal(expectedValue, payload.data, "Value read from channel should match what was sent")
	})

	t.Run("Send", func(t *testing.T) {
		assert := assert.New(t)

		// Create a new node with buffer size 1
		node := CreateNode[string](1)

		// Send a value using the Send method
		expectedValue := "test-value"
		node.Send(expectedValue)

		// Verify we can read the payload from the channel
		payload := <-node.ch
		assert.True(payload.ok, "payload ok flag should be true")
		assert.Equal(expectedValue, payload.data, "Value read from channel should match what was sent")
	})

	t.Run("Send and Read Integration", func(t *testing.T) {
		assert := assert.New(t)

		// Create a new node with buffer size 5
		node := CreateNode[string](5)

		// Send multiple values
		expectedValues := []string{"value1", "value2", "value3"}
		for _, val := range expectedValues {
			node.Send(val)
		}

		// Read and verify each value
		for _, expected := range expectedValues {
			payload := <-node.ch
			assert.True(payload.ok, "payload ok flag should be true")
			assert.Equal(expected, payload.data, "Values should be received in the same order they were sent")
		}
	})

	t.Run("ServeAndStop", func(t *testing.T) {
		assert := assert.New(t)

		node := CreateNode[string](5)

		var wg sync.WaitGroup
		processed := make(chan struct{})

		// Start Serve in a goroutine
		done := make(chan struct{})
		go func() {
			node.Serve(func(v string) {
				wg.Done()
				processed <- struct{}{}
			})
			close(done)
		}()

		// Expect Serve to process a single item
		wg.Add(1)
		node.Send("hello")
		<-processed // ensure function executed

		// Stop the node and ensure Serve exits
		node.Stop()

		select {
		case <-done:
			// Serve exited as expected
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("Serve did not exit after Stop() was called")
		}

		// Further sends should not block since Serve is no longer running; but channel still open
		// Verify sentinel payload present
		sentinel := <-node.ch
		assert.False(sentinel.ok, "expected sentinel payload after Stop")
	})
}
