package utils

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewNotifier(t *testing.T) {
	t.Run("create notifier with zero buffer", func(t *testing.T) {
		// Create a notifier with zero buffer
		notifier := NewNotifier(0)
		
		// Verify it's not nil and is a channel
		assert.NotNil(t, notifier, "Notifier should not be nil")
		
		// Close to prevent leaking
		notifier.Close()
	})

	t.Run("create notifier with specified buffer", func(t *testing.T) {
		// Create a notifier with a specific buffer size
		bufferSize := uint32(5)
		notifier := NewNotifier(bufferSize)
		
		// Verify it's not nil
		assert.NotNil(t, notifier, "Notifier should not be nil")
		
		// Send messages to fill buffer (shouldn't block because of buffer)
		for i := uint32(0); i < bufferSize; i++ {
			notifier.Send()
		}
		
		// Close to prevent leaking
		notifier.Close()
	})
}

func TestNotifierSend(t *testing.T) {
	t.Run("send to non-full channel", func(t *testing.T) {
		// Create a notifier with buffer
		notifier := NewNotifier(1)
		
		// Send message
		notifier.Send()
		
		// Verify channel has a message by trying to receive
		select {
		case _, ok := <-notifier:
			assert.True(t, ok, "Channel should have received a message")
		default:
			t.Fatal("Channel should have a message but none was received")
		}
		
		// Close to prevent leaking
		notifier.Close()
	})

	t.Run("send to full channel doesn't block", func(t *testing.T) {
		// Create a notifier with a specific buffer size
		bufferSize := uint32(1)
		notifier := NewNotifier(bufferSize)
		
		// Fill the buffer
		notifier.Send()
		
		// This should not block even though the buffer is full
		sendCompleted := make(chan struct{})
		go func() {
			notifier.Send() // This would block if default case wasn't handled
			close(sendCompleted)
		}()
		
		// Wait a short time to see if send completes
		select {
		case <-sendCompleted:
			// Test passed, the send didn't block
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Send to full channel blocked when it shouldn't")
		}
		
		// Close to prevent leaking
		notifier.Close()
	})
}

func TestNotifierReceive(t *testing.T) {
	t.Run("receive executes function", func(t *testing.T) {
		// Create a notifier with buffer
		notifier := NewNotifier(1)
		
		// Create a counter to be incremented by the function
		counter := 0
		var wg sync.WaitGroup
		wg.Add(1)
		
		// Start a goroutine to receive
		go func() {
			defer wg.Done()
			notifier.Receive(func() {
				counter++
			})
		}()
		
		// Send a message
		notifier.Send()
		
		// Close the notifier to complete the Receive loop
		notifier.Close()
		
		// Wait for the goroutine to finish
		wg.Wait()
		
		// Verify the function was executed
		assert.Equal(t, 1, counter, "Receive function should have been executed once")
	})

	t.Run("receive executes function multiple times", func(t *testing.T) {
		// Create a notifier with buffer
		notifier := NewNotifier(5)
		
		// Create a counter to be incremented by the function
		counter := 0
		var wg sync.WaitGroup
		wg.Add(1)
		
		// Start a goroutine to receive
		go func() {
			defer wg.Done()
			notifier.Receive(func() {
				counter++
			})
		}()
		
		// Send multiple messages
		messagesToSend := 3
		for i := 0; i < messagesToSend; i++ {
			notifier.Send()
		}
		
		// Close the notifier to complete the Receive loop
		notifier.Close()
		
		// Wait for the goroutine to finish
		wg.Wait()
		
		// Verify the function was executed for each message
		assert.Equal(t, messagesToSend, counter, "Receive function should have been executed for each message")
	})
}

func TestNotifierClose(t *testing.T) {
	t.Run("close terminates receive loop", func(t *testing.T) {
		// Create a notifier
		notifier := NewNotifier(1)
		
		// Set up a flag to track if receive loop completed
		receiveCompleted := false
		var wg sync.WaitGroup
		wg.Add(1)
		
		// Start a goroutine to receive
		go func() {
			defer wg.Done()
			notifier.Receive(func() {
				// Do nothing
			})
			receiveCompleted = true
		}()
		
		// Close the notifier
		err := notifier.Close()
		
		// Verify close didn't return an error
		assert.NoError(t, err, "Close should not return an error")
		
		// Wait for the goroutine to finish
		wg.Wait()
		
		// Verify the receive loop completed
		assert.True(t, receiveCompleted, "Receive loop should have completed after close")
	})

	t.Run("cannot send after close", func(t *testing.T) {
		// Create a notifier
		notifier := NewNotifier(1)
		
		// Close the notifier
		notifier.Close()
		
		// Set up a channel to detect panic
		panicOccurred := make(chan bool, 1)
		
		// Try to send to closed channel, which should panic
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicOccurred <- true
				} else {
					panicOccurred <- false
				}
			}()
			
			notifier.Send() // This should panic since channel is closed
		}()
		
		// Verify a panic occurred
		assert.True(t, <-panicOccurred, "Sending to closed notifier should cause panic")
	})
}
