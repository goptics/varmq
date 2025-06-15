package varmq

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHelperFunctions tests the helper functions for direct function submission
func TestHelperFunctions(t *testing.T) {

	t.Run("Func", func(t *testing.T) {
		t.Run("returns valid worker function", func(t *testing.T) {
			workerFunc := Func()
			assert.NotNil(t, workerFunc, "Func() should return a non-nil function")
		})

		t.Run("executes function correctly", func(t *testing.T) {
			var executed bool
			testFunc := func() {
				executed = true
			}

			// Create mock job
			mockJob := &job[func()]{
				data: testFunc,
			}

			// Execute the worker function
			workerFunc := Func()
			workerFunc(mockJob)

			assert.True(t, executed, "function should have been executed")
		})

		t.Run("integration with worker", func(t *testing.T) {
			var executed bool
			worker := NewWorker(Func())
			queue := worker.BindQueue()
			defer worker.Stop()

			// Add function
			job, ok := queue.Add(func() {
				executed = true
			})

			assert.True(t, ok, "job should be added successfully")
			assert.NotNil(t, job, "job should not be nil")
			worker.WaitUntilFinished()
			assert.True(t, executed, "function should have been executed by worker")
		})
	})

	t.Run("ErrFunc", func(t *testing.T) {
		t.Run("returns valid worker function", func(t *testing.T) {
			workerFunc := ErrFunc()
			assert.NotNil(t, workerFunc, "ErrFunc() should return a non-nil function")
		})

		t.Run("executes function that returns nil error", func(t *testing.T) {
			var executed bool
			testFunc := func() error {
				executed = true
				return nil
			}

			// Create mock job
			mockJob := &job[func() error]{
				data: testFunc,
			}

			// Execute the worker function
			workerFunc := ErrFunc()
			err := workerFunc(mockJob)

			assert.True(t, executed, "function should have been executed")
			assert.NoError(t, err, "should return nil error")
		})

		t.Run("executes function that returns error", func(t *testing.T) {
			expectedError := errors.New("test error")
			testFunc := func() error {
				return expectedError
			}

			// Create mock job
			mockJob := &job[func() error]{
				data: testFunc,
			}

			// Execute the worker function
			workerFunc := ErrFunc()
			err := workerFunc(mockJob)

			assert.Error(t, err, "should return error")
			assert.Equal(t, expectedError, err, "should return the exact error")
		})

		t.Run("integration with error worker", func(t *testing.T) {
			expectedError := errors.New("integration test error")
			worker := NewErrWorker(ErrFunc())
			queue := worker.BindQueue()

			defer worker.Stop()

			// Add function that returns error
			job, ok := queue.Add(func() error {
				return expectedError
			})

			assert.True(t, ok, "job should be added successfully")
			assert.NotNil(t, job, "job should not be nil")

			err := job.Err()
			assert.Error(t, err, "job should have error")
			assert.Equal(t, expectedError, err, "should return the exact error")
		})
	})

	t.Run("ResultFunc", func(t *testing.T) {
		t.Run("returns valid worker function for string type", func(t *testing.T) {
			workerFunc := ResultFunc[string]()
			assert.NotNil(t, workerFunc, "ResultFunc[string]() should return a non-nil function")
		})

		t.Run("executes function that returns result and nil error", func(t *testing.T) {
			expectedResult := "test result"
			testFunc := func() (string, error) {
				return expectedResult, nil
			}

			// Create mock job
			mockJob := &job[func() (string, error)]{
				data: testFunc,
			}

			// Execute the worker function
			workerFunc := ResultFunc[string]()
			result, err := workerFunc(mockJob)

			assert.NoError(t, err, "should return nil error")
			assert.Equal(t, expectedResult, result, "should return the exact result")
		})

		t.Run("executes function that returns error", func(t *testing.T) {
			expectedError := errors.New("test error")
			testFunc := func() (string, error) {
				return "", expectedError
			}

			// Create mock job
			mockJob := &job[func() (string, error)]{
				data: testFunc,
			}

			// Execute the worker function
			workerFunc := ResultFunc[string]()
			result, err := workerFunc(mockJob)

			assert.Error(t, err, "should return error")
			assert.Equal(t, expectedError, err, "should return the exact error")
			assert.Equal(t, "", result, "should return zero value for result on error")
		})

		t.Run("works with different types", func(t *testing.T) {
			t.Run("int type", func(t *testing.T) {
				expectedResult := 42
				testFunc := func() (int, error) {
					return expectedResult, nil
				}

				mockJob := &job[func() (int, error)]{
					data: testFunc,
				}

				workerFunc := ResultFunc[int]()
				result, err := workerFunc(mockJob)

				assert.NoError(t, err, "should return nil error")
				assert.Equal(t, expectedResult, result, "should return the exact result")
			})

			t.Run("struct type", func(t *testing.T) {
				type TestStruct struct {
					ID   int
					Name string
				}
				expectedResult := TestStruct{ID: 1, Name: "test"}
				testFunc := func() (TestStruct, error) {
					return expectedResult, nil
				}

				mockJob := &job[func() (TestStruct, error)]{
					data: testFunc,
				}

				workerFunc := ResultFunc[TestStruct]()
				result, err := workerFunc(mockJob)

				assert.NoError(t, err, "should return nil error")
				assert.Equal(t, expectedResult, result, "should return the exact result")
			})
		})

		t.Run("integration with result worker", func(t *testing.T) {
			expectedResult := "integration test result"
			worker := NewResultWorker(ResultFunc[string]())
			queue := worker.BindQueue()

			defer worker.Stop()

			// Add function that returns result
			job, ok := queue.Add(func() (string, error) {
				return expectedResult, nil
			})

			assert.True(t, ok, "job should be added successfully")
			assert.NotNil(t, job, "job should not be nil")

			result, err := job.Result()
			assert.NoError(t, err, "job should not have error")
			assert.Equal(t, expectedResult, result, "should return the exact result")
		})
	})
}
