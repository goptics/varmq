package varmq

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetrics(t *testing.T) {
	t.Run("NewMetrics", func(t *testing.T) {
		m := newMetrics()
		assert.NotNil(t, m)
		assert.Equal(t, uint64(0), m.Submitted())
		assert.Equal(t, uint64(0), m.Completed())
		assert.Equal(t, uint64(0), m.Successful())
		assert.Equal(t, uint64(0), m.Failed())
	})

	t.Run("IncrementMethods", func(t *testing.T) {
		m := newMetrics()

		m.incSubmitted()
		assert.Equal(t, uint64(1), m.Submitted())

		m.incCompleted()
		assert.Equal(t, uint64(1), m.Completed())

		m.incSuccessful()
		assert.Equal(t, uint64(1), m.Successful())

		m.incFailed()
		assert.Equal(t, uint64(1), m.Failed())
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		m := newMetrics()
		concurrency := 100
		iterations := 1000

		var wg sync.WaitGroup
		wg.Add(concurrency)

		for range concurrency {
			go func() {
				defer wg.Done()
				for range iterations {
					m.incSubmitted()
					m.incCompleted()
					m.incSuccessful()
					m.incFailed()
				}
			}()
		}

		wg.Wait()

		expected := uint64(concurrency * iterations)
		assert.Equal(t, expected, m.Submitted())
		assert.Equal(t, expected, m.Completed())
		assert.Equal(t, expected, m.Successful())
		assert.Equal(t, expected, m.Failed())
	})

	t.Run("Reset", func(t *testing.T) {
		m := newMetrics()
		m.incSubmitted()
		m.incCompleted()
		m.incSuccessful()
		m.incFailed()

		m.Reset()

		assert.Equal(t, uint64(0), m.Submitted())
		assert.Equal(t, uint64(0), m.Completed())
		assert.Equal(t, uint64(0), m.Successful())
		assert.Equal(t, uint64(0), m.Failed())
	})
}
