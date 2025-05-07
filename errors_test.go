package varmq

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

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

	// Run the tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := selectError(tc.errors...)
			assert.Equal(t, tc.expected, result)
		})
	}
}
