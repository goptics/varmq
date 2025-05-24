package pool

// import (
// 	"testing"
// 	"time"

// 	"github.com/stretchr/testify/assert"
// )

// func TestPoolNode(t *testing.T) {
// 	t.Run("Initialization", func(t *testing.T) {
// 		assert := assert.New(t)

// 		// Create a new poolNode using the factory function
// 		node := newPoolNode[string, int](1)

// 		// Check initial lastUsed time is zero
// 		assert.True(node.GetLastUsed().IsZero(), "lastUsed time should be zero for a newly created node")

// 		// Check channel is initialized
// 		assert.NotNil(node.ch, "channel should be initialized")
// 	})

// 	t.Run("UpdateLastUsed", func(t *testing.T) {
// 		assert := assert.New(t)

// 		// Create a new poolNode using the factory function
// 		node := newPoolNode[string, int](1)

// 		// Initial time should be zero
// 		assert.True(node.GetLastUsed().IsZero(), "lastUsed time should initially be zero")

// 		// Update the last used time
// 		before := time.Now()
// 		node.UpdateLastUsed()
// 		after := time.Now()

// 		// Verify lastUsed time was updated and is between before and after
// 		assert.False(node.GetLastUsed().IsZero(), "lastUsed time should no longer be zero")
// 		assert.True(node.GetLastUsed().After(before) || node.GetLastUsed().Equal(before),
// 			"lastUsed time should be after or equal to time before update")
// 		assert.True(node.GetLastUsed().Before(after) || node.GetLastUsed().Equal(after),
// 			"lastUsed time should be before or equal to time after update")
// 	})

// 	t.Run("GetLastUsed", func(t *testing.T) {
// 		assert := assert.New(t)

// 		// Create a new poolNode using the factory function
// 		node := newPoolNode[string, int](1)

// 		// Initial time should be zero
// 		initialTime := node.GetLastUsed()
// 		assert.True(initialTime.IsZero(), "initial time should be zero")

// 		// Update the time
// 		node.UpdateLastUsed()
// 		updatedTime := node.GetLastUsed()
// 		assert.False(updatedTime.IsZero(), "updated time should not be zero")
// 		assert.True(updatedTime.After(initialTime), "updated time should be after initial time")
// 	})

// 	t.Run("Close", func(t *testing.T) {
// 		assert := assert.New(t)

// 		// Create a new poolNode using the factory function
// 		node := newPoolNode[string, int](1)

// 		// Close the channel
// 		node.Close()

// 		// Verify the channel is closed by trying to send to it (should panic)
// 		defer func() {
// 			r := recover()
// 			assert.NotNil(r, "sending to closed channel should panic")
// 		}()

// 		// This should panic because the channel is closed
// 		node.ch <- nil
// 	})
// }
