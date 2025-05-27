package utils

import (
	"errors"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCpus(t *testing.T) {
	// Get the CPU count using the util function
	cpuCount := Cpus()

	// Get the CPU count directly from runtime
	expectedCpuCount := uint32(runtime.NumCPU())

	// Verify they match
	assert.Equal(t, expectedCpuCount, cpuCount, "Cpus() should return the number of logical CPUs on the system")
}

func TestWithSafe(t *testing.T) {
	t.Run("execute function without panic", func(t *testing.T) {
		// Define a function that doesn't panic
		fn := func() {
			// Do some work without panicking
			for i := range 10 {
				_ = i * i
			}
		}

		// Run the function with WithSafe
		err := WithSafe("non-panicking-function", fn)

		// Verify no error occurred
		assert.NoError(t, err, "WithSafe should not return an error when the function doesn't panic")
	})

	t.Run("recover from function with panic", func(t *testing.T) {
		// Define a function that will panic
		fn := func() {
			panic("intentional panic for testing")
		}

		// Run the function with WithSafe
		err := WithSafe("panicking-function", fn)

		// Verify an error was returned
		assert.Error(t, err, "WithSafe should return an error when the function panics")

		// Verify the error contains the correct information
		assert.Contains(t, err.Error(), "intentional panic for testing", "Error should contain the panic message")
		assert.Contains(t, err.Error(), "panicking-function", "Error should contain the function name")
	})
}

func TestGoWithSafe(t *testing.T) {
	t.Run("execute goroutine without panic", func(t *testing.T) {
		// Define a function that doesn't panic
		fn := func() {
			// Do some work without panicking
			for i := range 10 {
				_ = i * i
			}
		}

		// Run the function with GoWithSafe
		errChan := GoWithSafe("non-panicking-goroutine", fn)

		// Check the error channel
		err, ok := <-errChan
		assert.False(t, ok, "Channel should be closed")
		assert.Nil(t, err, "No error should be received for non-panicking function")
	})

	t.Run("recover from goroutine with panic", func(t *testing.T) {
		// Define a function that will panic
		fn := func() {
			panic("intentional goroutine panic for testing")
		}

		// Run the function with GoWithSafe
		errChan := GoWithSafe("panicking-goroutine", fn)

		// Check the error channel
		err, ok := <-errChan
		assert.True(t, ok, "Channel should have an error")
		assert.NotNil(t, err, "An error should be received")

		// Verify the error contains the correct information
		assert.Contains(t, err.Error(), "intentional goroutine panic for testing", "Error should contain the panic message")
		assert.Contains(t, err.Error(), "panicking-goroutine", "Error should contain the function name")

		// Verify that the channel is eventually closed
		_, ok = <-errChan
		assert.False(t, ok, "Channel should be closed after error is sent")
	})
}

func TestSelectError(t *testing.T) {
	// Define some test errors
	err1 := errors.New("error 1")
	err2 := errors.New("error 2")
	err3 := errors.New("error 3")

	// Test cases
	tests := []struct {
		name     string
		errors   []error
		expected error
	}{
		{
			name:     "all nil errors",
			errors:   []error{nil, nil, nil},
			expected: nil,
		},
		{
			name:     "first error non-nil",
			errors:   []error{err1, nil, nil},
			expected: err1,
		},
		{
			name:     "middle error non-nil",
			errors:   []error{nil, err2, nil},
			expected: err2,
		},
		{
			name:     "last error non-nil",
			errors:   []error{nil, nil, err3},
			expected: err3,
		},
		{
			name:     "multiple non-nil errors",
			errors:   []error{err1, err2, err3},
			expected: err1, // should return the first non-nil error
		},
		{
			name:     "no errors provided",
			errors:   []error{},
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := SelectError(tc.errors...)
			assert.Equal(t, tc.expected, result)
		})
	}
}
