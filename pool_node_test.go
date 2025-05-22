package varmq

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPoolNode(t *testing.T) {
	t.Run("Initialization", func(t *testing.T) {
		assert := assert.New(t)
		
		// Create a new poolNode
		node := poolNode[string, int]{
			ch: make(chan iJob[string, int], 1),
		}
		
		// Check initial lastUsed time is zero
		assert.True(node.lastUsed.IsZero(), "lastUsed time should be zero for a newly created node")
		
		// Check channel is initialized
		assert.NotNil(node.ch, "channel should be initialized")
	})

	t.Run("UpdateLastUsed", func(t *testing.T) {
		assert := assert.New(t)
		
		// Create a new poolNode
		node := poolNode[string, int]{
			ch: make(chan iJob[string, int], 1),
		}
		
		// Initial time should be zero
		assert.True(node.lastUsed.IsZero(), "lastUsed time should initially be zero")
		
		// Update the last used time
		before := time.Now()
		node.UpdateLastUsed()
		after := time.Now()
		
		// Verify lastUsed time was updated and is between before and after
		assert.False(node.lastUsed.IsZero(), "lastUsed time should no longer be zero")
		assert.True(node.lastUsed.After(before) || node.lastUsed.Equal(before), 
			"lastUsed time should be after or equal to time before update")
		assert.True(node.lastUsed.Before(after) || node.lastUsed.Equal(after), 
			"lastUsed time should be before or equal to time after update")
	})

	t.Run("Close", func(t *testing.T) {
		assert := assert.New(t)
		
		// Create a new poolNode
		node := poolNode[string, int]{
			ch: make(chan iJob[string, int], 1),
		}
		
		// Close the channel
		node.Close()
		
		// Verify the channel is closed by trying to send to it (should panic)
		defer func() {
			r := recover()
			assert.NotNil(r, "sending to closed channel should panic")
		}()
		
		// This should panic because the channel is closed
		node.ch <- nil
	})
}
